// values.ts — Per-field plausible mock values.
//
// The auto-mock generator builds Envelope objects from a (ViewDef +
// DomainDef) pair. This module is the type-driven leaf: given a
// DomainFieldDef, return a value that looks plausible in the
// rendered preview.
//
// Goals:
//   * Type-correct  — int fields get numbers, dates get ISO dates,
//                     bool fields get booleans, optionsRef fields
//                     get a value from the matching option list.
//   * Stable        — the same field always produces the same value
//                     within one envelope build, so the preview
//                     doesn't shuffle on every keystroke.
//   * Lightweight   — no faker dependency; a small hand-built table
//                     of names/cities/words is plenty for an editor
//                     preview.
//
// `mockValueForField` is deterministic when given the same `seed`.
// The envelope generator passes the row index as the seed so a
// list of three persons gets three different but reproducible
// mock rows.

import type { DomainFieldDef, FieldTypeDef } from "oos-dsls-ts";

/** Hand-rolled name pools — enough variety for a preview list. */
const FIRST_NAMES = [
	"Elena",
	"Markus",
	"Sophie",
	"Tobias",
	"Anna",
	"Lukas",
	"Mia",
	"Felix",
	"Hannah",
	"Jonas",
];

const LAST_NAMES = [
	"Kovač",
	"Hofer",
	"Berger",
	"Lehmann",
	"Schneider",
	"Becker",
	"Wagner",
	"Krüger",
	"Vogel",
	"Bauer",
];

const WORDS = [
	"Cloud-Architecture",
	"Migration",
	"DACH",
	"Roadmap",
	"Sprint-Planning",
	"Operations",
	"Stakeholder",
	"Review",
	"Kickoff",
	"Strategy",
];

/**
 * mockValueForField returns a plausible value for one field of a
 * domain. The shape matches what `loadEnvelope` accepts: scalars
 * stay scalar, the caller is responsible for wrapping into the
 * `{ content: { domain: { field: ... } } }` envelope.
 *
 * @param field  the field definition.
 * @param seed   row index (0-based). Used to pick deterministic
 *               variants from the name pools so every row in a
 *               table renders differently.
 * @param optionFor  resolver for `optionsRef` fields. Returns the
 *               first option's `value`, or undefined when no
 *               options exist for that ref. The envelope builder
 *               wires this up so values referenced from a Meta
 *               always match a real entry in the options list.
 */
export function mockValueForField(
	field: DomainFieldDef,
	seed: number,
	optionFor: (ref: string, seed: number) => string | undefined,
): unknown {
	if (field.optionsRef) {
		const v = optionFor(field.optionsRef, seed);
		if (v !== undefined) return v;
		// fall through to a typed default if the meta has no options
	}

	switch (field.type) {
		case "int":
			return mockInt(field.name, seed);
		case "float":
			return mockFloat(field.name, seed);
		case "bool":
			return seed % 2 === 0;
		case "date":
			return mockDate(seed, false);
		case "datetime":
			return mockDate(seed, true);
		case "text":
			return mockText(field.name, seed);
		case "string":
			return mockString(field.name, seed);
	}
}

/**
 * mockInt returns an integer that's roughly in the right ballpark
 * for the field's name. `id`/`age`/year-ish fields get small-to-
 * medium integers; everything else gets a generic small int.
 */
function mockInt(name: string, seed: number): number {
	const lower = name.toLowerCase();
	if (lower === "id") return seed + 1;
	if (lower.includes("age")) return 25 + ((seed * 7) % 50);
	if (lower.includes("year")) return 1990 + ((seed * 3) % 35);
	if (lower.includes("count") || lower.includes("qty")) {
		return ((seed + 1) * 3) % 25;
	}
	return seed * 7 + 12;
}

/**
 * mockFloat returns a floating-point value, larger for money-ish
 * names so currency formatting in the preview looks meaningful.
 */
function mockFloat(name: string, seed: number): number {
	const lower = name.toLowerCase();
	if (
		lower.includes("price") ||
		lower.includes("worth") ||
		lower.includes("amount") ||
		lower.includes("salary") ||
		lower.includes("cost")
	) {
		return 50_000 + seed * 75_000 + ((seed * 137) % 10_000);
	}
	return Number(((seed + 1) * 3.14).toFixed(2));
}

/**
 * mockDate returns an ISO-formatted date roughly `seed` days before
 * today, optionally including a time component for datetime fields.
 */
function mockDate(seed: number, withTime: boolean): string {
	const now = new Date("2026-04-15T10:00:00Z");
	now.setDate(now.getDate() - seed * 3);
	if (withTime) {
		return now.toISOString().slice(0, 19) + "Z";
	}
	return now.toISOString().slice(0, 10);
}

/**
 * mockString picks a value based on common field-name heuristics.
 * The matching is intentionally loose — any field with `name` in
 * its identifier gets a name, anything with `email` an email, etc.
 * Falls through to a generic phrase otherwise.
 */
function mockString(name: string, seed: number): string {
	const lower = name.toLowerCase();
	if (lower === "id") return String(seed + 1);
	if (lower.includes("first") && lower.includes("name")) {
		return pick(FIRST_NAMES, seed);
	}
	if (lower.includes("last") && lower.includes("name")) {
		return pick(LAST_NAMES, seed);
	}
	if (lower.includes("name")) {
		return `${pick(FIRST_NAMES, seed)} ${pick(LAST_NAMES, seed)}`;
	}
	if (lower.includes("email")) {
		const f = pick(FIRST_NAMES, seed).toLowerCase();
		const l = pick(LAST_NAMES, seed).toLowerCase().replace(/[^a-z]/g, "");
		return `${f}.${l}@example.com`;
	}
	if (lower.includes("phone")) {
		return `+49 ${89 + seed} ${1000 + seed * 17}`;
	}
	if (lower.includes("city") || lower.includes("town")) {
		return pick(["München", "Berlin", "Hamburg", "Köln", "Frankfurt"], seed);
	}
	if (lower.includes("country")) {
		return pick(["Deutschland", "Österreich", "Schweiz"], seed);
	}
	if (lower.includes("title")) {
		return `${pick(WORDS, seed)} ${pick(WORDS, seed + 1)}`;
	}
	if (lower.includes("url") || lower.includes("href")) {
		return `https://example.com/${seed}`;
	}
	return `${pick(WORDS, seed)} ${seed + 1}`;
}

/**
 * mockText is for longer free-form fields (`text`-typed). Strings
 * a couple of WORDS together so the preview shows a multi-line
 * value instead of a one-token blob.
 */
function mockText(_name: string, seed: number): string {
	return [
		pick(WORDS, seed),
		pick(WORDS, seed + 1),
		pick(WORDS, seed + 2),
		"–",
		pick(WORDS, seed + 3),
		pick(WORDS, seed + 4) + ".",
	].join(" ");
}

/** pick returns `arr[seed mod len]`. Stable for the same input. */
function pick<T>(arr: readonly T[], seed: number): T {
	return arr[((seed % arr.length) + arr.length) % arr.length]!;
}

/** Re-export used by callers that want to know the supported types. */
export type { FieldTypeDef };
