// envelope.ts — Build a complete preview Envelope from a (View, Domain) pair.
//
// The envelope shape is defined by `oos-ui-ts/envelope.ts`:
//
//   {
//     content: { <domainName>: { <field>: <value>, rows?: [...] } },
//     meta:    { <metaName>: [{value, label}, ...] }
//   }
//
// Decision tree for what `content[domain]` should look like:
//
//   * If the View body contains at least one Table whose rowSource
//     resolves to `<domain>.<something>` — typically `.rows` — we
//     build a list view: `{ rows: [...] }` with 4 mock rows, each
//     populated using `mockValueForField` for every field on the
//     domain.
//
//   * Otherwise we build a detail view: a single object with one
//     entry per field. The seed is `0`, so detail views always
//     show the same canonical first record.
//
// Both shapes can coexist (a view with a header detail and a
// related-rows table below) — in that case the rows live under
// `<domain>.rows` and the scalars at the top level of `<domain>`,
// matching the convention `loadEnvelope` already follows.
//
// Meta entries: every Meta declared on the bound Domain produces
// an option list under `meta.<metaName>`. Even Metas the View
// doesn't visibly use are included — it costs nothing and means
// authors can toggle dropdowns on or off without re-running the
// generator.

import type {
	BodyElementDef,
	DomainDef,
	DomainFieldDef,
	TabPaneDef,
	TableDef,
	ViewDef,
} from "oos-dsls-ts";

import type { EnvelopeObject } from "../envelope";
import { mockOptionsFor, type MockOption } from "./options";
import { mockValueForField } from "./values";

/** How many rows to fabricate when a Table is detected. */
const ROW_COUNT = 4;

/**
 * mockEnvelope produces a fully populated Envelope ready for
 * `loadEnvelope`. `view` and `domain` are required; the function
 * is pure and deterministic.
 *
 * The caller is responsible for parsing both — the preview pane
 * already runs the View parser and now also runs the Domain
 * parser when this generator is in play.
 */
export function mockEnvelope(view: ViewDef, domain: DomainDef): EnvelopeObject {
	const meta = buildMetaSection(domain);
	const optionFor = makeOptionResolver(domain, meta);

	// The bind paths inside the view quote the domain by its local
	// alias (e.g. `over person(p)` → `p.firstname`). For the mock
	// envelope to align with what the renderer reads, we key the
	// content section by that alias, and check tables against it.
	const primaryAlias =
		view.domains.find((d) => d.name === domain.name)?.alias ?? domain.name;

	const hasTable = containsTableForAlias(view.body, primaryAlias);
	const content: Record<string, unknown> = {};

	if (hasTable) {
		const rows: Record<string, unknown>[] = [];
		for (let i = 0; i < ROW_COUNT; i++) {
			rows.push(buildRow(domain, i, optionFor));
		}
		content[primaryAlias] = { rows, ...buildRow(domain, 0, optionFor) };
	} else {
		content[primaryAlias] = buildRow(domain, 0, optionFor);
	}

	return { content, meta } satisfies EnvelopeObject;
}

// ─── Section builders ────────────────────────────────────────────────

/**
 * buildRow generates one record's worth of mock data for the
 * given domain. Every field gets an entry, including readonly
 * ones — the renderer chooses what to display.
 */
function buildRow(
	domain: DomainDef,
	seed: number,
	optionFor: (ref: string, seed: number) => string | undefined,
): Record<string, unknown> {
	const row: Record<string, unknown> = {};
	for (const field of domain.fields) {
		row[field.name] = mockValueForField(field, seed, optionFor);
	}
	return row;
}

/**
 * buildMetaSection produces the `meta` half of the envelope: one
 * entry per declared Meta on the domain. Empty when the domain
 * has no Metas, which is fine — `loadEnvelope` ignores absent
 * sections.
 */
function buildMetaSection(domain: DomainDef): Record<string, MockOption[]> {
	const out: Record<string, MockOption[]> = {};
	for (const meta of domain.metas) {
		out[meta.name] = mockOptionsFor(meta);
	}
	return out;
}

/**
 * makeOptionResolver returns a function that mockValueForField
 * uses to fill `optionsRef` fields with a real option's value.
 * Falls back to undefined if the meta has no entries — the
 * caller's typed default takes over.
 *
 * Picking by `seed % length` means a row's optionsRef value is
 * stable across calls and varies row-to-row in tables.
 */
function makeOptionResolver(
	domain: DomainDef,
	meta: Record<string, MockOption[]>,
): (ref: string, seed: number) => string | undefined {
	return (ref, seed) => {
		const list = meta[ref];
		if (!list || list.length === 0) return undefined;
		const idx = ((seed % list.length) + list.length) % list.length;
		return list[idx]?.value;
	};
}

// ─── View body inspection ────────────────────────────────────────────

/**
 * containsTableForAlias walks the view body recursively and
 * returns true when at least one Table is bound to a row source
 * under the given alias (the FieldRef.domain part holds the
 * local alias inside `.view` files). Tabs and accordion items
 * are descended into.
 */
function containsTableForAlias(
	body:  readonly BodyElementDef[],
	alias: string,
): boolean {
	for (const el of body) {
		if (matches(el, alias)) return true;
	}
	return false;
}

function matches(el: BodyElementDef, alias: string): boolean {
	switch (el.kind) {
		case "table":
			return isTableForAlias(el, alias);
		case "section":
		case "stack":
		case "row":
		case "grid":
		case "card":
			return containsTableForAlias(el.body, alias);
		case "tabs":
			return el.tabs.some((tab: TabPaneDef) =>
				containsTableForAlias(tab.body, alias),
			);
		case "accordion":
			return el.items.some((item) =>
				containsTableForAlias(item.body, alias),
			);
		default:
			return false;
	}
}

function isTableForAlias(table: TableDef, alias: string): boolean {
	return table.rowSource.domain === alias;
}

// ─── Re-exports ──────────────────────────────────────────────────────

export type { DomainFieldDef };
