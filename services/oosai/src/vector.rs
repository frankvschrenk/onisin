//! pgvector encoding helpers.
//!
//! postgres has no native binding for the `vector` type, so we send
//! the textual input form `[1,2,3]` and cast to `::vector` server-side
//! — same approach as oos-embed-ts/pgvector.ts, dependency-free and
//! portable across drivers.

/// Serialises a float slice into pgvector's textual input form.
/// Non-finite values are rejected: pgvector cannot store NaN/Inf and a
/// silently broken vector would only surface as a query error later.
pub fn format_vector(values: &[f32]) -> anyhow::Result<String> {
    if values.is_empty() {
        anyhow::bail!("format_vector: empty input");
    }
    let mut out = String::with_capacity(values.len() * 8 + 2);
    out.push('[');
    for (i, v) in values.iter().enumerate() {
        if !v.is_finite() {
            anyhow::bail!("format_vector: non-finite value at index {i}");
        }
        if i > 0 {
            out.push(',');
        }
        out.push_str(&v.to_string());
    }
    out.push(']');
    Ok(out)
}
