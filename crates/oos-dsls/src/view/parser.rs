//! Recursive-descent parser for the onisin view-dsl.
//!
//! Parses straight into the minimal runtime `ViewDef`. Unlike the domain
//! parser it does *not* model the full body: the header (default flag,
//! name, title, `over` bindings) is parsed precisely, and the body is
//! walked brace-aware only to (a) validate that the block is balanced
//! and (b) collect the field names referenced by table columns. Widgets,
//! layout, toolbar and rich text are skipped — the frontend's own TS
//! parser renders those; the server only needs the chunk header and the
//! displayed-column list.

use super::lexer::{lex, Tok, Token};
use super::types::*;

/// Parses a `.view` source into a ViewDef, or the first error with its
/// source position. A header that does not parse, or an unbalanced /
/// trailing-junk body, is rejected so the backfill and index can skip a
/// broken view rather than embed a half-parsed one.
pub fn parse_view(source: &str) -> Result<ViewDef, ParseError> {
    let tokens = lex(source)?;
    Parser { toks: tokens, pos: 0 }.parse()
}

struct Parser {
    toks: Vec<Token>,
    pos: usize,
}

impl Parser {
    fn peek(&self) -> Option<&Tok> {
        self.toks.get(self.pos).map(|t| &t.tok)
    }

    fn peek_at(&self, off: usize) -> Option<&Tok> {
        self.toks.get(self.pos + off).map(|t| &t.tok)
    }

    fn err(&self, message: impl Into<String>) -> ParseError {
        let (line, col) = self
            .toks
            .get(self.pos)
            .map(|t| (t.line, t.col))
            .unwrap_or((0, 0));
        ParseError { line, col, message: message.into() }
    }

    fn at_kw(&self, kw: &str) -> bool {
        matches!(self.peek(), Some(Tok::Ident(s)) if s == kw)
    }

    fn eat(&mut self, want: &Tok, label: &str) -> Result<(), ParseError> {
        if self.peek() == Some(want) {
            self.pos += 1;
            Ok(())
        } else {
            Err(self.err(format!("expected {label}")))
        }
    }

    fn eat_kw(&mut self, kw: &str) -> Result<(), ParseError> {
        if self.at_kw(kw) {
            self.pos += 1;
            Ok(())
        } else {
            Err(self.err(format!("expected '{kw}'")))
        }
    }

    fn ident(&mut self) -> Result<String, ParseError> {
        if let Some(Tok::Ident(s)) = self.peek() {
            let s = s.clone();
            self.pos += 1;
            Ok(s)
        } else {
            Err(self.err("expected identifier"))
        }
    }

    fn string(&mut self) -> Result<String, ParseError> {
        if let Some(Tok::Str(s)) = self.peek() {
            let s = s.clone();
            self.pos += 1;
            Ok(s)
        } else {
            Err(self.err("expected a string"))
        }
    }

    fn parse(mut self) -> Result<ViewDef, ParseError> {
        // (default?) view NAME "title" over BINDING (, BINDING)* {
        let default = if self.at_kw("default") {
            self.pos += 1;
            true
        } else {
            false
        };
        self.eat_kw("view")?;
        let name = self.ident()?;
        let title = self.string()?;
        self.eat_kw("over")?;

        let mut domains = vec![self.domain_binding(true)?];
        while self.peek() == Some(&Tok::Comma) {
            self.pos += 1;
            domains.push(self.domain_binding(false)?);
        }

        self.eat(&Tok::LBrace, "'{'")?;
        let table_fields = self.collect_table_fields()?;

        Ok(ViewDef { name, title, domains, default, table_fields })
    }

    /// One `over` entry: `name` or `name(alias)`. A missing alias falls
    /// back to the name so single-domain views keep alias == name.
    fn domain_binding(&mut self, primary: bool) -> Result<ViewDomainBinding, ParseError> {
        let name = self.ident()?;
        let alias = if self.peek() == Some(&Tok::LParen) {
            self.pos += 1;
            let a = self.ident()?;
            self.eat(&Tok::RParen, "')'")?;
            a
        } else {
            name.clone()
        };
        Ok(ViewDomainBinding { name, alias, primary })
    }

    /// Walks the view body from just after the opening `{` to the
    /// matching `}`, returning the de-duplicated field names of every
    /// table column in first-seen order.
    ///
    /// Why a structural walk rather than parsing each element: the only
    /// body content the backend reads is table columns; everything else
    /// (widgets, layout, toolbar) is skipped. We still count braces so an
    /// unbalanced or trailing-junk source is rejected as a parse error,
    /// and we only collect `column` references while inside a `table { }`
    /// body (tracked by depth) so a field literally named `column`
    /// elsewhere can never be mistaken for the keyword.
    fn collect_table_fields(&mut self) -> Result<Vec<String>, ParseError> {
        let mut depth: usize = 1; // the view block's own `{` is already eaten
        let mut pending_table = false; // saw `table`, waiting for its `{`
        let mut table_body_depth: Option<usize> = None;
        let mut seen: Vec<String> = Vec::new();

        while let Some(tok) = self.peek() {
            match tok {
                Tok::LBrace => {
                    depth += 1;
                    if pending_table {
                        table_body_depth = Some(depth);
                        pending_table = false;
                    }
                    self.pos += 1;
                }
                Tok::RBrace => {
                    if table_body_depth == Some(depth) {
                        table_body_depth = None;
                    }
                    depth -= 1;
                    self.pos += 1;
                    if depth == 0 {
                        break;
                    }
                }
                Tok::Ident(s) if s == "table" => {
                    pending_table = true;
                    self.pos += 1;
                }
                Tok::Ident(s) if s == "column" && table_body_depth == Some(depth) => {
                    self.pos += 1;
                    // A column is `column <domain>.<field> "caption" ...`.
                    // Collect the field half; tolerate a malformed column
                    // rather than abort the whole parse.
                    if let (Some(Tok::Ident(_)), Some(Tok::Dot), Some(Tok::Ident(field))) =
                        (self.peek(), self.peek_at(1), self.peek_at(2))
                    {
                        let field = field.clone();
                        if !seen.iter().any(|f| f == &field) {
                            seen.push(field);
                        }
                        self.pos += 3;
                    }
                }
                _ => {
                    self.pos += 1;
                }
            }
        }

        if depth != 0 {
            return Err(self.err("unterminated view block, expected '}'"));
        }
        if self.peek().is_some() {
            return Err(self.err("unexpected trailing input after view block"));
        }
        Ok(seen)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Real seed views (verbatim from oos.view).
    const NOTE_LIST: &str = r#"
// note_list.view
view note_list "Notizen" over note {
  auto_refresh on note.changed
  toolbar {
    new "Neu" -> note_detail as tab
    refresh "Aktualisieren"
  }
  table -> note.rows {
    on_select -> note_detail bind=note.id as tab
    column note.id         "ID"    width=60
    column note.title      "Titel" width=300
    column note.created_at "Datum" width=160 format=datetime:short
  }
}
"#;

    const NOTE_DETAIL: &str = r#"
view note_detail "Notiz — Detail" over note {
  toolbar {
    save
    delete confirm="Notiz wirklich löschen?"
    exit
  }
  section "Notiz" p=md {
    text "ID"        -> note.id         readonly
    text "Person ID" -> note.person_id  readonly
  }
}
"#;

    #[test]
    fn parses_list_view_with_table_columns() {
        let v = parse_view(NOTE_LIST).expect("note_list should parse");
        assert_eq!(v.name, "note_list");
        assert_eq!(v.title, "Notizen");
        assert!(!v.default);
        assert_eq!(v.domains.len(), 1);
        assert_eq!(v.domains[0].name, "note");
        assert_eq!(v.domains[0].alias, "note");
        assert!(v.domains[0].primary);
        // Only column fields, in source order; rowSource (note.rows) and
        // the on_select bind (note.id) are not collected.
        assert_eq!(v.table_fields, vec!["id", "title", "created_at"]);
    }

    #[test]
    fn detail_view_has_no_table_fields() {
        let v = parse_view(NOTE_DETAIL).expect("note_detail should parse");
        assert_eq!(v.name, "note_detail");
        assert_eq!(v.title, "Notiz — Detail");
        assert!(v.table_fields.is_empty());
    }

    #[test]
    fn default_flag_and_multi_domain_aliases() {
        let src = r#"default view m "M" over person(p), address(a) {
            table -> p.rows { column p.id "ID" column a.city "Stadt" }
        }"#;
        let v = parse_view(src).unwrap();
        assert!(v.default);
        assert_eq!(v.domains.len(), 2);
        assert_eq!(v.domains[0].name, "person");
        assert_eq!(v.domains[0].alias, "p");
        assert!(v.domains[0].primary);
        assert_eq!(v.domains[1].alias, "a");
        assert!(!v.domains[1].primary);
        assert_eq!(v.table_fields, vec!["id", "city"]);
    }

    #[test]
    fn column_fields_are_deduped_first_seen() {
        let src = r#"view v "V" over d {
            table -> d.rows { column d.id "A" column d.name "B" column d.id "C" }
        }"#;
        let v = parse_view(src).unwrap();
        assert_eq!(v.table_fields, vec!["id", "name"]);
    }

    #[test]
    fn rejects_unbalanced_block() {
        let err = parse_view(r#"view v "V" over d { table -> d.rows { column d.id "A" "#)
            .unwrap_err();
        assert!(err.message.contains("unterminated"));
    }

    #[test]
    fn reports_position_on_missing_over() {
        // line 2: `view x "T"` then `{` with no `over`.
        let err = parse_view("\nview x \"T\" {}").unwrap_err();
        assert_eq!(err.line, 2);
    }

    #[test]
    fn serializes_with_ts_compatible_keys() {
        let v = parse_view(NOTE_LIST).unwrap();
        let json = serde_json::to_string(&v).unwrap();
        assert!(json.contains("\"tableFields\""));
        assert!(json.contains("\"primary\""));
    }
}
