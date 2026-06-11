//! Gemma 4's tool grammar: rendering declarations, calls and responses into
//! the checkpoint's native syntax, and parsing the model's calls back out.
//!
//! The grammar comes from the checkpoint's chat_template.jinja, which is the
//! ground truth for this format. It is JSON-shaped but not JSON: strings are
//! quoted with the dedicated `<|"|>` token instead of `"`, keys are bare,
//! and object keys are emitted in sorted order (the template dictsorts
//! everywhere). Everything in here is pure string work, so it carries unit
//! tests that run without a checkpoint.

use anyhow::{anyhow, bail, Result};
use oos_infer::openai::ToolFunction;
use serde_json::Value;

/// The string-quote marker; a single dedicated token (id 52) in the gemma4
/// tokenizer, so quoted content survives decoding only when special tokens
/// are kept.
const QUOTE: &str = "<|\"|>";

/// Render one advertised function into the `declaration:` body that goes
/// between `<|tool>` and `<tool|>`, mirroring the template's
/// format_function_declaration macro. One deliberate divergence: the
/// parameters' `type` field is always emitted (defaulting to OBJECT), because
/// the template only closes the parameters brace on that branch and a schema
/// without `type` would render unbalanced.
pub fn render_declaration(f: &ToolFunction) -> String {
    let mut s = format!(
        "declaration:{}{{description:{QUOTE}{}{QUOTE}",
        f.name, f.description
    );
    if let Some(params) = &f.parameters {
        s.push_str(",parameters:{");
        if let Some(props) = params.get("properties").and_then(Value::as_object) {
            s.push_str("properties:{");
            s.push_str(&render_properties(props));
            s.push_str("},");
        }
        if let Some(req) = params.get("required").and_then(Value::as_array) {
            s.push_str("required:[");
            let items: Vec<String> = req
                .iter()
                .filter_map(Value::as_str)
                .map(|r| format!("{QUOTE}{r}{QUOTE}"))
                .collect();
            s.push_str(&items.join(","));
            s.push_str("],");
        }
        let ty = params
            .get("type")
            .and_then(Value::as_str)
            .unwrap_or("object");
        s.push_str(&format!("type:{QUOTE}{}{QUOTE}}}", ty.to_uppercase()));
    }
    s.push('}');
    s
}

/// One property map, keys sorted, each as `key:{...,type:<|"|>T<|"|>}`;
/// the template's format_parameters macro reduced to the schema subset the
/// OpenAI tools we serve actually use (description, enum, nested object
/// properties/required, array items, nullable).
fn render_properties(props: &serde_json::Map<String, Value>) -> String {
    let mut keys: Vec<&String> = props.keys().collect();
    keys.sort();
    let mut parts = Vec::with_capacity(keys.len());
    for key in keys {
        let v = &props[key];
        let ty = v
            .get("type")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_uppercase();
        let mut bits: Vec<String> = Vec::new();
        if let Some(d) = v.get("description").and_then(Value::as_str) {
            bits.push(format!("description:{QUOTE}{d}{QUOTE}"));
        }
        if ty == "STRING" {
            if let Some(e) = v.get("enum") {
                bits.push(format!("enum:{}", render_argument(e)));
            }
        }
        if ty == "ARRAY" {
            if let Some(items) = v.get("items").and_then(Value::as_object) {
                let mut item_keys: Vec<&String> = items.keys().collect();
                item_keys.sort();
                let mut item_bits: Vec<String> = Vec::new();
                for ik in item_keys {
                    let iv = &items[ik];
                    match ik.as_str() {
                        "properties" => {
                            if let Some(p) = iv.as_object() {
                                item_bits.push(format!("properties:{{{}}}", render_properties(p)));
                            }
                        }
                        "required" => {
                            let reqs: Vec<String> = iv
                                .as_array()
                                .map(|a| {
                                    a.iter()
                                        .filter_map(Value::as_str)
                                        .map(|r| format!("{QUOTE}{r}{QUOTE}"))
                                        .collect()
                                })
                                .unwrap_or_default();
                            item_bits.push(format!("required:[{}]", reqs.join(",")));
                        }
                        "type" => {
                            let t = iv.as_str().unwrap_or("").to_uppercase();
                            item_bits.push(format!("type:{QUOTE}{t}{QUOTE}"));
                        }
                        _ => item_bits.push(format!("{ik}:{}", render_argument(iv))),
                    }
                }
                bits.push(format!("items:{{{}}}", item_bits.join(",")));
            }
        }
        if v.get("nullable").and_then(Value::as_bool) == Some(true) {
            bits.push("nullable:true".to_string());
        }
        if ty == "OBJECT" {
            if let Some(p) = v.get("properties").and_then(Value::as_object) {
                bits.push(format!("properties:{{{}}}", render_properties(p)));
            }
            if let Some(req) = v.get("required").and_then(Value::as_array) {
                let reqs: Vec<String> = req
                    .iter()
                    .filter_map(Value::as_str)
                    .map(|r| format!("{QUOTE}{r}{QUOTE}"))
                    .collect();
                bits.push(format!("required:[{}]", reqs.join(",")));
            }
        }
        bits.push(format!("type:{QUOTE}{ty}{QUOTE}"));
        parts.push(format!("{key}:{{{}}}", bits.join(",")));
    }
    parts.join(",")
}

/// Render a JSON value in the grammar's argument form (the template's
/// format_argument with escape_keys=False): strings between `<|"|>` markers,
/// bare keys, sorted object keys, bools and numbers verbatim.
pub fn render_argument(v: &Value) -> String {
    match v {
        Value::String(s) => format!("{QUOTE}{s}{QUOTE}"),
        Value::Bool(b) => b.to_string(),
        Value::Number(n) => n.to_string(),
        Value::Null => "null".to_string(),
        Value::Object(map) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            let parts: Vec<String> = keys
                .iter()
                .map(|k| format!("{k}:{}", render_argument(&map[*k])))
                .collect();
            format!("{{{}}}", parts.join(","))
        }
        Value::Array(items) => {
            let parts: Vec<String> = items.iter().map(render_argument).collect();
            format!("[{}]", parts.join(","))
        }
    }
}

/// Render a tool call's OpenAI `arguments` (a JSON-encoded object string)
/// into the grammar's `key:value,...` body. A non-object or unparseable
/// arguments string is inserted verbatim, mirroring the template's string
/// branch.
pub fn render_call_args(arguments: &str) -> String {
    match serde_json::from_str::<Value>(arguments) {
        Ok(Value::Object(map)) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            let parts: Vec<String> = keys
                .iter()
                .map(|k| format!("{k}:{}", render_argument(&map[*k])))
                .collect();
            parts.join(",")
        }
        _ => arguments.to_string(),
    }
}

/// Render one tool result into the `<|tool_response>...<tool_response|>`
/// block. The OpenAI path carries results as message content strings, and
/// the template wraps a string result as `{value:<|"|>...<|"|>}` without
/// parsing it -- mirrored here, faithful over clever.
pub fn render_response_block(name: &str, content: &str) -> String {
    format!("<|tool_response>response:{name}{{value:{QUOTE}{content}{QUOTE}}}<tool_response|>")
}

/// Parse one decoded tool-call span (`call:NAME{...}`, markers excluded,
/// special tokens kept) into the function name and a JSON-encoded arguments
/// object, converting the grammar back into JSON the OpenAI wire expects.
pub fn parse_call_span(span: &str) -> Result<(String, String)> {
    let rest = span
        .trim()
        .strip_prefix("call:")
        .ok_or_else(|| anyhow!("tool-call span has no call: prefix: {span:?}"))?;
    let brace = rest
        .find('{')
        .ok_or_else(|| anyhow!("tool-call span has no argument block: {span:?}"))?;
    let name = rest[..brace].trim().to_string();
    let body = rest[brace..].trim();
    if !body.ends_with('}') {
        bail!("tool-call span has an unterminated argument block: {span:?}");
    }
    let args = parse_pseudo_object(&body[1..body.len() - 1])?;
    Ok((name, args.to_string()))
}

/// Parse the inside of a `{...}` argument block into a JSON object.
fn parse_pseudo_object(body: &str) -> Result<Value> {
    let mut p = Parser { s: body, i: 0 };
    let v = p.object_body()?;
    p.skip_ws();
    if p.i < p.s.len() {
        bail!("trailing input after argument object: {:?}", &p.s[p.i..]);
    }
    Ok(v)
}

/// Recursive-descent parser for the argument grammar. Strings are delimited
/// by the `<|"|>` marker (no nesting, no escapes -- the template inserts
/// content verbatim, so the parser scans to the next marker); everything
/// else is bare up to the next structural character.
struct Parser<'a> {
    s: &'a str,
    i: usize,
}

impl<'a> Parser<'a> {
    fn skip_ws(&mut self) {
        while self.i < self.s.len() && self.s.as_bytes()[self.i].is_ascii_whitespace() {
            self.i += 1;
        }
    }

    fn eat(&mut self, c: u8) -> Result<()> {
        self.skip_ws();
        if self.i < self.s.len() && self.s.as_bytes()[self.i] == c {
            self.i += 1;
            Ok(())
        } else {
            bail!(
                "expected {:?} at offset {} in {:?}",
                c as char,
                self.i,
                self.s
            );
        }
    }

    /// `key:value(,key:value)*` -- the body of an object, braces consumed by
    /// the caller.
    fn object_body(&mut self) -> Result<Value> {
        let mut map = serde_json::Map::new();
        self.skip_ws();
        while self.i < self.s.len() {
            let key = self.key()?;
            self.eat(b':')?;
            let value = self.value()?;
            map.insert(key, value);
            self.skip_ws();
            if self.i < self.s.len() && self.s.as_bytes()[self.i] == b',' {
                self.i += 1;
                self.skip_ws();
            } else {
                break;
            }
        }
        Ok(Value::Object(map))
    }

    fn key(&mut self) -> Result<String> {
        self.skip_ws();
        if self.s[self.i..].starts_with(QUOTE) {
            return self.quoted();
        }
        let start = self.i;
        while self.i < self.s.len() && self.s.as_bytes()[self.i] != b':' {
            self.i += 1;
        }
        let key = self.s[start..self.i].trim();
        if key.is_empty() {
            bail!("empty key at offset {start} in {:?}", self.s);
        }
        Ok(key.to_string())
    }

    fn value(&mut self) -> Result<Value> {
        self.skip_ws();
        if self.s[self.i..].starts_with(QUOTE) {
            return Ok(Value::String(self.quoted()?));
        }
        match self.s.as_bytes().get(self.i) {
            Some(b'{') => {
                self.i += 1;
                let v = self.object_body()?;
                self.eat(b'}')?;
                Ok(v)
            }
            Some(b'[') => {
                self.i += 1;
                let mut items = Vec::new();
                self.skip_ws();
                if self.s.as_bytes().get(self.i) == Some(&b']') {
                    self.i += 1;
                    return Ok(Value::Array(items));
                }
                loop {
                    items.push(self.value()?);
                    self.skip_ws();
                    match self.s.as_bytes().get(self.i) {
                        Some(b',') => {
                            self.i += 1;
                        }
                        Some(b']') => {
                            self.i += 1;
                            break;
                        }
                        _ => bail!("unterminated array at offset {} in {:?}", self.i, self.s),
                    }
                }
                Ok(Value::Array(items))
            }
            Some(_) => self.bare(),
            None => bail!("expected a value at end of {:?}", self.s),
        }
    }

    /// The content between one `<|"|>` pair, taken verbatim.
    fn quoted(&mut self) -> Result<String> {
        self.i += QUOTE.len();
        match self.s[self.i..].find(QUOTE) {
            Some(end) => {
                let out = self.s[self.i..self.i + end].to_string();
                self.i += end + QUOTE.len();
                Ok(out)
            }
            None => bail!("unterminated string at offset {} in {:?}", self.i, self.s),
        }
    }

    /// A bare token up to the next structural character: true/false/null,
    /// a number, or -- as a lenient fallback for a model that skipped the
    /// quote markers -- a plain string.
    fn bare(&mut self) -> Result<Value> {
        let start = self.i;
        while self.i < self.s.len() && !matches!(self.s.as_bytes()[self.i], b',' | b'}' | b']') {
            self.i += 1;
        }
        let tok = self.s[start..self.i].trim();
        Ok(match tok {
            "true" => Value::Bool(true),
            "false" => Value::Bool(false),
            "null" => Value::Null,
            _ => match tok.parse::<i64>() {
                Ok(n) => Value::Number(n.into()),
                Err(_) => match tok.parse::<f64>() {
                    Ok(f) => serde_json::Number::from_f64(f)
                        .map(Value::Number)
                        .unwrap_or_else(|| Value::String(tok.to_string())),
                    Err(_) => Value::String(tok.to_string()),
                },
            },
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn declaration_renders_sorted_with_required_and_type() {
        let f: ToolFunction = serde_json::from_value(serde_json::json!({
            "name": "oos_schema_search",
            "description": "Search domain schemas",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search phrase"}
                },
                "required": ["query"]
            }
        }))
        .unwrap();
        let expected = concat!(
            "declaration:oos_schema_search{description:<|\"|>Search domain schemas<|\"|>",
            ",parameters:{properties:{query:{description:<|\"|>Search phrase<|\"|>",
            ",type:<|\"|>STRING<|\"|>}},required:[<|\"|>query<|\"|>]",
            ",type:<|\"|>OBJECT<|\"|>}}"
        );
        assert_eq!(render_declaration(&f), expected);
    }

    #[test]
    fn call_args_render_dictsorted() {
        let rendered = render_call_args(r#"{"zeta":1,"alpha":"x","flag":true}"#);
        assert_eq!(rendered, "alpha:<|\"|>x<|\"|>,flag:true,zeta:1");
    }

    #[test]
    fn parse_simple_call() {
        let (name, args) =
            parse_call_span("call:oos_schema_search{query:<|\"|>person<|\"|>}").unwrap();
        assert_eq!(name, "oos_schema_search");
        assert_eq!(args, r#"{"query":"person"}"#);
    }

    #[test]
    fn parse_quoted_content_keeps_structural_chars() {
        let (_, args) = parse_call_span("call:f{note:<|\"|>a, {b}: [c]<|\"|>,n:42}").unwrap();
        assert_eq!(args, r#"{"n":42,"note":"a, {b}: [c]"}"#);
    }

    #[test]
    fn parse_nested_objects_arrays_and_bare_values() {
        let (_, args) = parse_call_span(
            "call:f{opts:{deep:true,n:1.5},tags:[<|\"|>a<|\"|>,<|\"|>b<|\"|>],empty:[]}",
        )
        .unwrap();
        assert_eq!(
            args,
            r#"{"empty":[],"opts":{"deep":true,"n":1.5},"tags":["a","b"]}"#
        );
    }

    #[test]
    fn parse_unquoted_string_fallback() {
        // A model skipping the quote markers should still yield a usable
        // string argument rather than a parse failure.
        let (_, args) = parse_call_span("call:f{query:person}").unwrap();
        assert_eq!(args, r#"{"query":"person"}"#);
    }

    #[test]
    fn response_block_wraps_string_as_value() {
        assert_eq!(
            render_response_block("f", "{\"ok\":true}"),
            "<|tool_response>response:f{value:<|\"|>{\"ok\":true}<|\"|>}<tool_response|>"
        );
    }
}
