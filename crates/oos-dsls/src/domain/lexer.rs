//! Tokenizer for the onisin domain-dsl.
//!
//! Keywords are not lexed as distinct tokens: they come through as
//! `Ident` and the parser matches them by text where it expects them.
//! This sidesteps the reserved-word problem (a field could in principle
//! be named like a positional keyword such as `order_by`) and keeps the
//! token set tiny. The trade is that the lexer is slightly more
//! permissive than the Langium original, which never rejects a source
//! the grammar would have accepted — only the reverse, which does not
//! matter for editor-authored sources.

use super::types::ParseError;

/// A lexical token kind. Strings arrive already unquoted and unescaped,
/// matching Langium's default STRING value converter, so the parser and
/// the runtime defs never see the surrounding quotes.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Tok {
    Ident(String),
    /// Numeric literal kept as its raw source text — ExampleDef stores
    /// numbers stringified anyway, so there is no point parsing to f64
    /// and back.
    Number(String),
    Str(String),
    LBrace,
    RBrace,
    LBracket,
    RBracket,
    Colon,
    Comma,
    At,
    Eq,
    Arrow,
}

/// A token plus the 1-based position of its first character, used for
/// parse-error messages.
#[derive(Debug, Clone)]
pub struct Token {
    pub tok: Tok,
    pub line: usize,
    pub col: usize,
}

struct Lexer {
    chars: Vec<char>,
    i: usize,
    line: usize,
    col: usize,
}

impl Lexer {
    fn new(src: &str) -> Self {
        Lexer { chars: src.chars().collect(), i: 0, line: 1, col: 1 }
    }

    fn peek(&self) -> Option<char> {
        self.chars.get(self.i).copied()
    }

    fn peek2(&self) -> Option<char> {
        self.chars.get(self.i + 1).copied()
    }

    /// Consumes one char, advancing the line/col cursor.
    fn bump(&mut self) -> Option<char> {
        let c = self.peek()?;
        self.i += 1;
        if c == '\n' {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
        Some(c)
    }

    fn err(&self, message: String) -> ParseError {
        ParseError { line: self.line, col: self.col, message }
    }

    /// Skips whitespace and both comment styles. Loops because a comment
    /// can be followed by more whitespace and vice versa.
    fn skip_trivia(&mut self) -> Result<(), ParseError> {
        loop {
            match self.peek() {
                Some(c) if c.is_whitespace() => {
                    self.bump();
                }
                Some('/') if self.peek2() == Some('/') => {
                    while let Some(c) = self.peek() {
                        if c == '\n' {
                            break;
                        }
                        self.bump();
                    }
                }
                Some('/') if self.peek2() == Some('*') => {
                    self.bump();
                    self.bump();
                    loop {
                        match self.peek() {
                            None => return Err(self.err("unterminated block comment".into())),
                            Some('*') if self.peek2() == Some('/') => {
                                self.bump();
                                self.bump();
                                break;
                            }
                            _ => {
                                self.bump();
                            }
                        }
                    }
                }
                _ => return Ok(()),
            }
        }
    }

    fn lex_ident(&mut self) -> Tok {
        let mut s = String::new();
        while let Some(c) = self.peek() {
            if c == '_' || c.is_ascii_alphanumeric() {
                s.push(c);
                self.bump();
            } else {
                break;
            }
        }
        Tok::Ident(s)
    }

    fn lex_number(&mut self) -> Tok {
        let mut s = String::new();
        if self.peek() == Some('-') {
            s.push('-');
            self.bump();
        }
        while let Some(c) = self.peek() {
            if c.is_ascii_digit() {
                s.push(c);
                self.bump();
            } else {
                break;
            }
        }
        if self.peek() == Some('.') && self.peek2().map(|c| c.is_ascii_digit()).unwrap_or(false) {
            s.push('.');
            self.bump();
            while let Some(c) = self.peek() {
                if c.is_ascii_digit() {
                    s.push(c);
                    self.bump();
                } else {
                    break;
                }
            }
        }
        Tok::Number(s)
    }

    /// Lexes a double-quoted string, unescaping as Langium would. Unknown
    /// escapes pass the escaped char through verbatim rather than
    /// erroring — lenient on purpose, since the source is editor-authored.
    fn lex_string(&mut self) -> Result<Tok, ParseError> {
        self.bump(); // opening quote
        let mut s = String::new();
        loop {
            match self.bump() {
                None => return Err(self.err("unterminated string literal".into())),
                Some('"') => return Ok(Tok::Str(s)),
                Some('\\') => match self.bump() {
                    None => return Err(self.err("unterminated string literal".into())),
                    Some('n') => s.push('\n'),
                    Some('t') => s.push('\t'),
                    Some('r') => s.push('\r'),
                    Some(other) => s.push(other),
                },
                Some(c) => s.push(c),
            }
        }
    }

    fn run(&mut self) -> Result<Vec<Token>, ParseError> {
        let mut out = Vec::new();
        loop {
            self.skip_trivia()?;
            let (line, col) = (self.line, self.col);
            let Some(c) = self.peek() else { break };

            let tok = if c == '_' || c.is_ascii_alphabetic() {
                self.lex_ident()
            } else if c.is_ascii_digit()
                || (c == '-' && self.peek2().map(|d| d.is_ascii_digit()).unwrap_or(false))
            {
                self.lex_number()
            } else if c == '"' {
                self.lex_string()?
            } else if c == '-' && self.peek2() == Some('>') {
                self.bump();
                self.bump();
                Tok::Arrow
            } else {
                self.bump();
                match c {
                    '{' => Tok::LBrace,
                    '}' => Tok::RBrace,
                    '[' => Tok::LBracket,
                    ']' => Tok::RBracket,
                    ':' => Tok::Colon,
                    ',' => Tok::Comma,
                    '@' => Tok::At,
                    '=' => Tok::Eq,
                    other => {
                        return Err(ParseError {
                            line,
                            col,
                            message: format!("unexpected character '{other}'"),
                        })
                    }
                }
            };
            out.push(Token { tok, line, col });
        }
        Ok(out)
    }
}

/// Tokenizes a `.domain` source into a flat token stream.
pub fn lex(src: &str) -> Result<Vec<Token>, ParseError> {
    Lexer::new(src).run()
}
