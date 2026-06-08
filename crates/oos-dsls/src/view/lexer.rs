//! Tokenizer for the onisin view-dsl.
//!
//! Same approach as the domain lexer: keywords are not distinct tokens,
//! they arrive as `Ident` and the parser matches them by text. This
//! avoids the reserved-word problem (the view grammar lets domain field
//! names collide with widget/layout keywords like `email` or `time`) and
//! keeps the token set tiny.
//!
//! It is a strict superset of the domain lexer: the view grammar adds
//! parentheses (the `over person(p)` alias form) and dots (the
//! `<domain>.<field>` reference form), which the domain lexer never
//! needed. We keep a separate lexer rather than share one so the
//! verified domain tokenizer stays untouched.

use super::types::ParseError;

/// A lexical token kind. Strings arrive already unquoted and unescaped,
/// matching Langium's default STRING value converter.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Tok {
    Ident(String),
    /// Numeric literal kept as raw source text — the view parser never
    /// inspects numbers (widths, spacing, min/max), so there is no point
    /// parsing to a number and back.
    Number(String),
    Str(String),
    LBrace,
    RBrace,
    LParen,
    RParen,
    Dot,
    Colon,
    Comma,
    Eq,
    Arrow,
}

/// A token plus the 1-based position of its first character.
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

    /// Skips whitespace and both comment styles.
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

    /// Lexes a double-quoted string, unescaping as Langium would.
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
                    '(' => Tok::LParen,
                    ')' => Tok::RParen,
                    '.' => Tok::Dot,
                    ':' => Tok::Colon,
                    ',' => Tok::Comma,
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

/// Tokenizes a `.view` source into a flat token stream.
pub fn lex(src: &str) -> Result<Vec<Token>, ParseError> {
    Lexer::new(src).run()
}
