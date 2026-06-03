//! Recursive-descent parser for the onisin domain-dsl.
//!
//! Parses straight into the runtime `DomainDef` — there is no separate
//! AST + mapper step as in the Langium original, because the mapping is
//! a trivial one-pass fold and an intermediate tree would only add a
//! layer to read through. The grammar is small and keyword-led, so the
//! parser is plain LL(1): each member is chosen by its leading keyword.

use super::lexer::{lex, Tok, Token};
use super::types::*;

/// Parses a `.domain` source into a DomainDef, or the first error with
/// its source position. Total over the grammar: any accepted source
/// yields a DomainDef, any rejected one a ParseError.
pub fn parse_domain(source: &str) -> Result<DomainDef, ParseError> {
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

    /// Position used for an error at the current token, or the end of
    /// input when the stream is exhausted.
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

    fn parse(mut self) -> Result<DomainDef, ParseError> {
        self.eat_kw("domain")?;
        let name = self.ident()?;
        self.eat_kw("from")?;
        let source = self.ident()?;
        self.eat(&Tok::At, "'@'")?;
        let dsn = self.ident()?;
        self.eat(&Tok::LBrace, "'{'")?;

        let mut d = DomainDef {
            name,
            source,
            dsn,
            permissions: Vec::new(),
            fields: Vec::new(),
            relations: Vec::new(),
            metas: Vec::new(),
            ai_hints: Vec::new(),
            aliases: Vec::new(),
        };

        while self.peek() != Some(&Tok::RBrace) {
            if self.peek().is_none() {
                return Err(self.err("unexpected end of input, expected '}'"));
            }
            self.member(&mut d)?;
        }
        self.eat(&Tok::RBrace, "'}'")?;

        if self.peek().is_some() {
            return Err(self.err("unexpected trailing input after domain block"));
        }

        // Belt-and-suspenders: the ID terminal forbids '.', so a name can
        // never actually start with 'global.'; the guard stays in case a
        // future grammar allows dotted names, matching the Bun validator.
        if d.name.starts_with("global.") {
            return Err(ParseError {
                line: 1,
                col: 1,
                message: "domain names starting with 'global.' are reserved".into(),
            });
        }

        Ok(d)
    }

    /// Dispatches one domain member by its leading keyword.
    fn member(&mut self, d: &mut DomainDef) -> Result<(), ParseError> {
        if self.at_kw("permission") {
            let p = self.permission()?;
            d.permissions.push(p);
        } else if self.at_kw("field") {
            let f = self.field()?;
            d.fields.push(f);
        } else if self.at_kw("relation") {
            let r = self.relation()?;
            d.relations.push(r);
        } else if self.at_kw("meta") {
            let m = self.meta()?;
            d.metas.push(m);
        } else if self.at_kw("ai") {
            let h = self.ai_hint()?;
            d.ai_hints.push(h);
        } else if self.at_kw("aliases") {
            self.alias_list(d)?;
        } else {
            return Err(self.err(
                "expected a domain member (permission, field, relation, meta, ai, aliases)",
            ));
        }
        Ok(())
    }

    fn permission(&mut self) -> Result<PermissionDef, ParseError> {
        self.eat_kw("permission")?;
        let role = self.ident()?;
        let mut actions = vec![self.permission_action()?];
        while self.peek() == Some(&Tok::Comma) {
            self.pos += 1;
            actions.push(self.permission_action()?);
        }
        Ok(PermissionDef { role, actions })
    }

    fn permission_action(&mut self) -> Result<PermissionAction, ParseError> {
        let kw = self.ident()?;
        match kw.as_str() {
            "read" => Ok(PermissionAction::Read),
            "write" => Ok(PermissionAction::Write),
            "delete" => Ok(PermissionAction::Delete),
            other => Err(self.err(format!("unknown permission action '{other}'"))),
        }
    }

    fn field(&mut self) -> Result<DomainFieldDef, ParseError> {
        self.eat_kw("field")?;
        let name = self.ident()?;
        self.eat(&Tok::Colon, "':'")?;
        let field_type = self.field_type()?;

        let mut read_only = false;
        let mut filterable = false;
        let mut options_ref = None;
        loop {
            if self.at_kw("readonly") {
                self.pos += 1;
                read_only = true;
            } else if self.at_kw("filterable") {
                self.pos += 1;
                filterable = true;
            } else if self.at_kw("options") {
                self.pos += 1;
                self.eat(&Tok::Eq, "'='")?;
                options_ref = Some(self.ident()?);
            } else {
                break;
            }
        }

        let mut examples = Vec::new();
        if self.peek() == Some(&Tok::LBrace) {
            self.pos += 1;
            while self.peek() != Some(&Tok::RBrace) {
                if self.peek().is_none() {
                    return Err(self.err("unterminated example block"));
                }
                examples.push(self.example()?);
            }
            self.eat(&Tok::RBrace, "'}'")?;
        }

        Ok(DomainFieldDef { name, field_type, read_only, filterable, options_ref, examples })
    }

    fn field_type(&mut self) -> Result<FieldType, ParseError> {
        let kw = self.ident()?;
        match kw.as_str() {
            "int" => Ok(FieldType::Int),
            "float" => Ok(FieldType::Float),
            "string" => Ok(FieldType::String),
            "text" => Ok(FieldType::Text),
            "bool" => Ok(FieldType::Bool),
            "date" => Ok(FieldType::Date),
            "datetime" => Ok(FieldType::Datetime),
            other => Err(self.err(format!("unknown field type '{other}'"))),
        }
    }

    fn example(&mut self) -> Result<ExampleDef, ParseError> {
        self.eat_kw("example")?;
        let op = self.example_op()?;
        let (value, value_is_string) = match self.peek() {
            Some(Tok::Str(_)) => (self.string()?, true),
            Some(Tok::Number(n)) => {
                let n = n.clone();
                self.pos += 1;
                (n, false)
            }
            _ => return Err(self.err("expected a string or number value")),
        };
        let description = self.string()?;
        Ok(ExampleDef { op, value, value_is_string, description })
    }

    fn example_op(&mut self) -> Result<ExampleOp, ParseError> {
        let kw = self.ident()?;
        match kw.as_str() {
            "eq" => Ok(ExampleOp::Eq),
            "ne" => Ok(ExampleOp::Ne),
            "lt" => Ok(ExampleOp::Lt),
            "le" => Ok(ExampleOp::Le),
            "gt" => Ok(ExampleOp::Gt),
            "ge" => Ok(ExampleOp::Ge),
            "like" => Ok(ExampleOp::Like),
            "in" => Ok(ExampleOp::In),
            other => Err(self.err(format!("unknown example operator '{other}'"))),
        }
    }

    fn relation(&mut self) -> Result<RelationDef, ParseError> {
        self.eat_kw("relation")?;
        let name = self.ident()?;
        self.eat(&Tok::Colon, "':'")?;
        let kind = self.relation_kind()?;
        let target = self.ident()?;
        self.eat_kw("bind")?;
        self.eat(&Tok::Eq, "'='")?;
        let local_field = self.ident()?;
        self.eat(&Tok::Arrow, "'->'")?;
        let foreign_field = self.ident()?;
        Ok(RelationDef { name, kind, target, local_field, foreign_field })
    }

    fn relation_kind(&mut self) -> Result<RelationKind, ParseError> {
        let kw = self.ident()?;
        match kw.as_str() {
            "has_many" => Ok(RelationKind::HasMany),
            "has_one" => Ok(RelationKind::HasOne),
            "belongs_to" => Ok(RelationKind::BelongsTo),
            other => Err(self.err(format!("unknown relation kind '{other}'"))),
        }
    }

    fn meta(&mut self) -> Result<MetaDef, ParseError> {
        self.eat_kw("meta")?;
        let name = self.ident()?;
        self.eat_kw("from")?;
        let table = self.ident()?;
        let value_field = self.ident()?;
        let label_field = self.ident()?;
        // order_by and via are positional and optional; matched by text
        // so they need not be globally reserved.
        let order_by = if self.at_kw("order_by") {
            self.pos += 1;
            Some(self.ident()?)
        } else {
            None
        };
        let dsn = if self.at_kw("via") {
            self.pos += 1;
            Some(self.ident()?)
        } else {
            None
        };
        Ok(MetaDef { name, table, value_field, label_field, order_by, dsn })
    }

    fn ai_hint(&mut self) -> Result<AiHintDef, ParseError> {
        self.eat_kw("ai")?;
        let name = self.string()?;
        let body = self.string()?;
        Ok(AiHintDef { name, body })
    }

    /// Multiple `aliases [...]` clauses concatenate; blank entries are
    /// dropped so downstream consumers never see empties.
    fn alias_list(&mut self, d: &mut DomainDef) -> Result<(), ParseError> {
        self.eat_kw("aliases")?;
        self.eat(&Tok::LBracket, "'['")?;
        let push = |s: String, d: &mut DomainDef| {
            let t = s.trim();
            if !t.is_empty() {
                d.aliases.push(t.to_string());
            }
        };
        let first = self.string()?;
        push(first, d);
        while self.peek() == Some(&Tok::Comma) {
            self.pos += 1;
            let v = self.string()?;
            push(v, d);
        }
        self.eat(&Tok::RBracket, "']'")?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // The real note.domain shipped in the demo seed — the canonical
    // exercise of permissions, typed fields, both example value kinds,
    // ai hints and an alias list.
    const NOTE: &str = r#"
// note.domain — domain-dsl example
domain note from note@demo {
  permission admin   read, write, delete
  permission manager read, write
  permission user    read
  field id         : int      readonly
  field person_id  : int      readonly filterable {
    example eq 42 "Alle Notizen der Person mit id 42"
  }
  field title      : string   filterable {
    example like "Meeting" "Titel enthält 'Meeting'"
  }
  field body       : text
  field created_at : datetime readonly
  ai "scope"         "Notizen gehören immer zu einer Person."
  ai "edit_behavior" "Nur title und body sind editierbar."
  aliases [ "notizen", "anmerkungen", "bemerkungen" ]
}
"#;

    #[test]
    fn parses_note_domain() {
        let d = parse_domain(NOTE).expect("note.domain should parse");
        assert_eq!(d.name, "note");
        assert_eq!(d.source, "note");
        assert_eq!(d.dsn, "demo");

        assert_eq!(d.permissions.len(), 3);
        assert_eq!(d.permissions[0].role, "admin");
        assert_eq!(
            d.permissions[0].actions,
            vec![PermissionAction::Read, PermissionAction::Write, PermissionAction::Delete]
        );
        assert_eq!(d.permissions[2].actions, vec![PermissionAction::Read]);

        assert_eq!(d.fields.len(), 5);
        let id = &d.fields[0];
        assert_eq!(id.name, "id");
        assert_eq!(id.field_type, FieldType::Int);
        assert!(id.read_only);
        assert!(!id.filterable);

        let person = &d.fields[1];
        assert!(person.read_only && person.filterable);
        assert_eq!(person.examples.len(), 1);
        assert_eq!(person.examples[0].op, ExampleOp::Eq);
        assert_eq!(person.examples[0].value, "42");
        assert!(!person.examples[0].value_is_string);

        let title = &d.fields[2];
        assert_eq!(title.field_type, FieldType::String);
        assert_eq!(title.examples[0].op, ExampleOp::Like);
        assert_eq!(title.examples[0].value, "Meeting");
        assert!(title.examples[0].value_is_string);

        assert_eq!(d.fields[4].field_type, FieldType::Datetime);

        assert_eq!(d.ai_hints.len(), 2);
        assert_eq!(d.ai_hints[0].name, "scope");
        assert_eq!(d.aliases, vec!["notizen", "anmerkungen", "bemerkungen"]);
    }

    #[test]
    fn parses_relation_meta_and_options() {
        let src = r#"
domain person from person@demo {
  field dept_id : int filterable options = departments
  relation notes : has_many note bind = id -> person_id
  meta departments from department id name order_by name via demo
}
"#;
        let d = parse_domain(src).unwrap();
        assert_eq!(d.fields[0].options_ref.as_deref(), Some("departments"));
        assert_eq!(d.relations.len(), 1);
        let r = &d.relations[0];
        assert_eq!(r.kind, RelationKind::HasMany);
        assert_eq!(r.target, "note");
        assert_eq!(r.local_field, "id");
        assert_eq!(r.foreign_field, "person_id");
        let m = &d.metas[0];
        assert_eq!(m.table, "department");
        assert_eq!(m.value_field, "id");
        assert_eq!(m.label_field, "name");
        assert_eq!(m.order_by.as_deref(), Some("name"));
        assert_eq!(m.dsn.as_deref(), Some("demo"));
    }

    #[test]
    fn empty_alias_entries_are_dropped() {
        let d = parse_domain(r#"domain x from x@d { aliases [ "a", "  ", "b" ] }"#).unwrap();
        assert_eq!(d.aliases, vec!["a", "b"]);
    }

    #[test]
    fn rejects_unknown_field_type() {
        let err = parse_domain("domain x from x@d { field f : money }").unwrap_err();
        assert!(err.message.contains("unknown field type"));
    }

    #[test]
    fn rejects_missing_close_brace() {
        let err = parse_domain("domain x from x@d { field f : int").unwrap_err();
        assert!(err.message.contains("expected"));
    }

    #[test]
    fn reports_position_on_error() {
        // 'from' missing after the name on line 2.
        let err = parse_domain("\ndomain x y@d {}").unwrap_err();
        assert_eq!(err.line, 2);
    }

    #[test]
    fn serializes_with_ts_compatible_keys() {
        let d = parse_domain(NOTE).unwrap();
        let json = serde_json::to_string(&d).unwrap();
        // The keys the Bun mapper emitted, that downstream consumers key on.
        assert!(json.contains("\"aiHints\""));
        assert!(json.contains("\"valueIsString\""));
        assert!(json.contains("\"readOnly\""));
        assert!(json.contains("\"type\":\"int\""));
    }
}
