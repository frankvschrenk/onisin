// renderer/aliases.ts — Canonical alias list for one domain.
//
// Aliases are short German phrases that boost retrieval against
// queries phrased in everyday language: "alle person", "person
// Liste", "person bearbeiten". The renderer in llm-chunk.ts emits
// these into the embedded chunk; the agent-side resolver reads
// them when classifying user input.
//
// Keeping the list in one file means the chunk and the resolver
// can never disagree about what counts as an alias for a domain.

import type { DomainDef } from "../types";

/**
 * domainAliases returns the deterministic alias list for one
 * domain. Includes the bare domain name itself so a substring
 * match against user input ("zeig mir alle person") catches the
 * literal mention without a separate code path.
 *
 * Plural-form heuristic: domain names in OOS are conventionally
 * singular ("person", "note", "address"). Users phrase questions
 * in plural ("zeige mir alle Personen", "lade die Notizen"). The
 * resolver's containsWord() honours word boundaries, so a singular
 * alias like "person" never matches inside the plural "personen".
 * To close that gap we synthesise the most likely German plural
 * for every singular alias and emit both forms. The heuristic is
 * not linguistically perfect — German plurals are a mess of
 * suffixes and umlauts — but it covers the dominant weak-noun
 * patterns ("Person → Personen", "Note → Noten", "Adresse → Adressen")
 * which are exactly the words an OOS domain tends to be.
 *
 * Author-supplied `aliases [...]` from the DSL are appended on
 * top of the heuristic forms, with the same list/all/show/edit
 * phrasings expanded around each one. This is the deterministic
 * escape hatch for irregular plurals ("Häuser"), English
 * borrowings where the heuristic guesses wrong ("Notizen", not
 * "Noten") and synonyms outside the morphological space
 * altogether ("Mitarbeiter" for "person"). The clause never
 * replaces the heuristic, so existing domains keep working
 * unchanged; authors only add the words the heuristic cannot
 * reach.
 *
 * Order is stable: singular first, then list/all/show/edit/detail,
 * then the same set with the pluralised name, then each authored
 * alias with its own list/all/show/edit set. Stable order matters
 * for chunk caching and human inspection. Duplicates arising from
 * an authored alias coinciding with a heuristic form are filtered
 * at the end.
 */
export function domainAliases(def: DomainDef): string[] {
	const singular = def.name;
	const plural   = germanPlural(singular);

	const out: string[] = [
		singular,
		`${singular} Liste`,
		`alle ${singular}`,
		`${singular} anzeigen`,
		`${singular} bearbeiten`,
		`${singular} Detail`,
	];

	// Append the pluralised forms when they are actually different
	// — guards against names that already end in a German plural
	// suffix and would produce a no-op duplicate.
	if (plural && plural !== singular) {
		out.push(
			plural,
			`${plural} Liste`,
			`alle ${plural}`,
			`${plural} anzeigen`,
			`${plural} bearbeiten`,
		);
	}

	// Author-supplied aliases. Each one expands into the same
	// phrasing family as the canonical name; this gives the
	// resolver something concrete to match against without
	// having to re-implement the phrase generator on the consumer
	// side. Bare aliases like "Mitarbeiter" still match because
	// containsWord checks the full alias string against the input.
	for (const alias of def.aliases) {
		const a = alias.trim();
		if (!a) continue;
		out.push(
			a,
			`${a} Liste`,
			`alle ${a}`,
			`${a} anzeigen`,
			`${a} bearbeiten`,
		);
	}

	// Drop duplicates while keeping first occurrence — matters when
	// an authored alias accidentally repeats a heuristic form, or
	// when two `aliases [...]` clauses overlap.
	return Array.from(new Set(out));
}

/**
 * germanPlural produces the most likely plural form of a singular
 * noun. Heuristic, German-specific, lower-cased input expected.
 *
 * Rules, in order of specificity:
 *   - already ends in -en or -er or -s — assume already plural
 *   - ends in -e         → append -n   (Note → Noten, Adresse → Adressen)
 *   - ends in vowel      → append -s   (Auto → Autos, Foto → Fotos)
 *   - otherwise          → append -en  (Person → Personen, Kunde would be -n but starts non-e so falls here; trade-off)
 *
 * Casing is preserved on the first letter of the input. This is
 * deliberate: domain names are lower-case by convention but the
 * resolver lower-cases everything before matching, so casing of
 * the alias does not affect lookup. We keep the input shape so
 * the rendered chunk reads naturally.
 *
 * Returns the input unchanged when no rule applies (defensive
 * branch — every input is expected to hit one of the rules
 * above, but we never want this helper to return empty or undefined).
 */
function germanPlural(singular: string): string {
	if (!singular) return singular;
	const lower = singular.toLowerCase();

	// Already plural-shaped — leave as is. Catches "personen",
	// "kunden", "noten" if a domain author uses a plural name.
	if (lower.endsWith("en")) return singular;
	if (lower.endsWith("er")) return singular;
	if (lower.endsWith("s"))  return singular;

	// Schwa ending — typical for Romance loans absorbed into
	// German weak declension. "Note" → "Noten", "Adresse" →
	// "Adressen", "Kunde" → "Kunden".
	if (lower.endsWith("e")) return `${singular}n`;

	// Vowel-final non-schwa — usually Latin/Greek loans or
	// abbreviated forms. "Auto" → "Autos", "Konto" → "Konten" is
	// linguistically more correct but rarer in domain names; -s
	// is the safer bet for the OOS use case.
	const last = lower[lower.length - 1]!;
	if ("aiou".includes(last)) return `${singular}s`;

	// Default: append -en. Catches the dominant German pattern
	// for foreign-origin or specialist nouns: Person → Personen,
	// Kontakt → Kontakte (slight miss, but the embedding catches
	// that via cosine), Mitarbeiter is already plural-shaped above.
	return `${singular}en`;
}
