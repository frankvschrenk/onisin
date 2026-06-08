//! Canonical alias list for one domain (Rust port of renderer/aliases.ts).
//!
//! Aliases are short German phrases that boost retrieval against queries
//! phrased in everyday language ("alle person", "person Liste"). The chunk
//! renderer emits them and the agent-side resolver matches against them;
//! keeping the generator in one place means the two can never disagree.

use std::collections::HashSet;

use super::types::DomainDef;

/// Deterministic alias list for one domain: the bare name, German
/// list/all/show/edit/detail phrasings, a heuristic plural with the same
/// phrasings, then every author-supplied alias expanded the same way.
/// Duplicates removed keeping first occurrence; order is stable so the
/// rendered chunk hashes identically across rebuilds.
pub fn domain_aliases(def: &DomainDef) -> Vec<String> {
    let singular = def.name.clone();
    let plural = german_plural(&singular);

    let mut out: Vec<String> = vec![
        singular.clone(),
        format!("{singular} Liste"),
        format!("alle {singular}"),
        format!("{singular} anzeigen"),
        format!("{singular} bearbeiten"),
        format!("{singular} Detail"),
    ];

    // Pluralised forms, only when actually different from the singular
    // (guards against names that already end in a plural suffix).
    if !plural.is_empty() && plural != singular {
        out.push(plural.clone());
        out.push(format!("{plural} Liste"));
        out.push(format!("alle {plural}"));
        out.push(format!("{plural} anzeigen"));
        out.push(format!("{plural} bearbeiten"));
    }

    // Author-supplied aliases, each expanded into the same phrasing
    // family. The deterministic escape hatch for irregular plurals and
    // synonyms the heuristic cannot reach ("Mitarbeiter" for person).
    for alias in &def.aliases {
        let a = alias.trim();
        if a.is_empty() {
            continue;
        }
        out.push(a.to_string());
        out.push(format!("{a} Liste"));
        out.push(format!("alle {a}"));
        out.push(format!("{a} anzeigen"));
        out.push(format!("{a} bearbeiten"));
    }

    dedup_keep_first(out)
}

/// Drops duplicates while keeping the first occurrence, preserving order.
fn dedup_keep_first(items: Vec<String>) -> Vec<String> {
    let mut seen: HashSet<String> = HashSet::new();
    let mut out = Vec::with_capacity(items.len());
    for it in items {
        if seen.insert(it.clone()) {
            out.push(it);
        }
    }
    out
}

/// Most likely German plural of a singular noun. Heuristic, not perfect:
/// it covers the dominant weak-noun patterns OOS domain names fall into
/// (Person -> Personen, Note -> Noten). Rules in order of specificity:
/// already -en/-er/-s (assume plural) -> unchanged; -e -> +n; vowel -> +s;
/// otherwise -> +en.
fn german_plural(singular: &str) -> String {
    if singular.is_empty() {
        return singular.to_string();
    }
    let lower = singular.to_lowercase();
    if lower.ends_with("en") || lower.ends_with("er") || lower.ends_with('s') {
        return singular.to_string();
    }
    if lower.ends_with('e') {
        return format!("{singular}n");
    }
    // Safe to unwrap: the empty case returned above.
    let last = lower.chars().last().unwrap();
    if "aiou".contains(last) {
        return format!("{singular}s");
    }
    format!("{singular}en")
}
