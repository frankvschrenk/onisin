// renderer/llm-chunk.ts — DomainDef → LLM-friendly text chunk.
//
// Direct port of `renderChunk()` and its writer functions from
// `oosp/pluginsrv/store/schema_chunk.go`. The contract for downstream
// consumers (granite-embedding + pgvector + frontend LLM) is preserved:
// the same shape of chunk produces the same retrieval behaviour.
//
// Why this lives in the DSL package and not the backend: the renderer
// is a pure function (DomainDef → string) with no I/O. Keeping it
// next to the parser and mapper means the frontend LLM-debug tooling
// can call it directly to show "what does the system know about
// person?" without round-tripping to oosp.
//
// The chunk is composed in numbered sections, each emitted by a
// dedicated writer function. Order matters: the embedding model sees
// header → fields → filter examples → relations → metas → permissions
// → AI hints, in that order, every time. Stability across rebuilds
// is part of the contract.

import type {
	AiHintDef,
	DomainDef,
	DomainFieldDef,
	ExampleDef,
	FieldTypeDef,
	MetaDef,
	PermissionDef,
	RelationDef,
} from "../types";
import { domainAliases } from "./aliases";
import {
	findOperator,
	formatExampleValue,
	operatorsForType,
	renderFilterArg,
	type FilterOp,
} from "./operators";

/**
 * renderLLMChunk produces the structured, human-readable description
 * of a domain that gets embedded into pgvector for LLM retrieval.
 *
 * The output is deterministic: same DomainDef → same string, byte for
 * byte. That property lets the embedding pipeline detect "no change,
 * skip re-embed" by hashing the chunk text.
 */
export function renderLLMChunk(def: DomainDef): string {
	const parts: string[] = [];
	writeHeader(parts, def);
	writeAliases(parts, def);
	writeFields(parts, def);
	writeFilterExamples(parts, def);
	writeRelations(parts, def);
	writeMetas(parts, def);
	writeDropdownFieldMapping(parts, def);
	writePermissions(parts, def);
	writeAiHints(parts, def);
	return parts.join("");
}

// ─── Section: header ─────────────────────────────────────────────────

/** writeHeader emits the one-line identity of the domain. */
function writeHeader(parts: string[], def: DomainDef): void {
	parts.push(`Domain: ${def.name}\n`);
	parts.push(`Source: ${def.source}@${def.dsn}\n`);
}

// ─── Section: aliases ────────────────────────────────────────────────

/**
 * writeAliases emits German alias lines that boost retrieval for
 * queries phrased in German. The legacy renderer derived these from
 * `_list`/`_detail` suffixes on context names. The new domain DSL has
 * no such suffix — the domain is just `person` — so we emit a
 * uniform set that covers list, detail and edit phrasings.
 */
function writeAliases(parts: string[], def: DomainDef): void {
	// Skip the bare name — domainAliases includes it for the
	// resolver, but the chunk's "Alias:" line is supposed to add
	// retrieval-friendly variants on top of the name that already
	// appears in the header.
	const variants = domainAliases(def).filter((a) => a !== def.name);
	parts.push(`Alias: ${variants.join(", ")}\n`);
}

// ─── Section: fields ─────────────────────────────────────────────────

/** writeFields lists every field with type and filterable/readonly attrs. */
function writeFields(parts: string[], def: DomainDef): void {
	if (def.fields.length === 0) return;
	const descs = def.fields.map(describeField);
	parts.push(`Fields: ${descs.join(" | ")}\n`);

	// Compact "ALLOWED query fields" line — all field names, comma-
	// separated. Tells the LLM exactly which columns it may select.
	const names = def.fields.map((f) => f.name).join(", ");
	parts.push(`ALLOWED query fields (ONLY these, no others): ${names}\n`);

	// Canonical "fetch all" GraphQL query against the domain.
	const selection = def.fields.map((f) => f.name).join(" ");
	parts.push(`GraphQL query all: { ${def.name} { ${selection} } }\n`);
}

/** describeField renders one field as `name (type, attr1, attr2, ...)`. */
function describeField(f: DomainFieldDef): string {
	const attrs: string[] = [];
	if (f.readOnly) attrs.push("readonly");
	if (f.filterable) attrs.push("filterable");
	if (f.optionsRef) attrs.push(`options=${f.optionsRef}`);
	if (attrs.length === 0) {
		return `${f.name} (${f.type})`;
	}
	return `${f.name} (${f.type}, ${attrs.join(", ")})`;
}

// ─── Section: filter examples ────────────────────────────────────────

/**
 * writeFilterExamples emits filter examples for every filterable
 * field. Two passes per field: first the type-driven defaults (so
 * the LLM sees the syntax for every supported operator), then the
 * author overrides from `example` blocks (so the chunk feels
 * specific and accurate).
 */
function writeFilterExamples(parts: string[], def: DomainDef): void {
	const filterable = def.fields.filter((f) => f.filterable);
	if (filterable.length === 0) return;

	const names = filterable.map((f) => f.name).join(", ");
	parts.push(`Filterable fields: ${names}\n`);

	// The selection set used inside every example query. Using the
	// full field list is closer to what the LLM actually emits at
	// runtime than guessing a smaller "list_fields" subset.
	const selection = def.fields.map((f) => f.name).join(" ");

	for (const f of filterable) {
		// Pass 1: typed defaults.
		for (const op of operatorsForType(f.type)) {
			parts.push(renderFilterExampleLine(def.name, f, op, op.sampleValue, selection));
		}

		// Pass 2: author overrides.
		for (const ex of f.examples) {
			const op = findOperator(f.type, ex.op);
			if (!op) continue; // Unknown operator for this type — skip silently.
			const value = formatExampleValue(f.type, ex.value, ex.valueIsString);
			parts.push(renderFilterExampleOverride(def.name, f, op, ex, value, selection));
		}
	}

	// Multi-filter example: show how to combine two filters in one query.
	// Without this the LLM never sees the combined-args syntax and fails
	// when the user asks for two conditions at once.
	if (filterable.length >= 2) {
		parts.push(renderMultiFilterExample(def.name, filterable, selection));
	}
}

/**
 * renderMultiFilterExample picks the first two filterable fields and
 * combines their default operators into a single GraphQL query, e.g.:
 *   { person(age_gt: 0, city_eq: "value") { … } }
 *
 * This teaches the LLM that multiple filter args are comma-separated
 * inside the same parenthesis — not two separate queries.
 */
function renderMultiFilterExample(
	domainName: string,
	filterable: DomainFieldDef[],
	selection: string,
): string {
	const [a, b] = filterable;
	if (!a || !b) return "";
	const opA = operatorsForType(a.type)[0];
	const opB = operatorsForType(b.type)[0];
	if (!opA || !opB) return "";
	const argA = renderFilterArg(a.name, opA.suffix, opA.sampleValue);
	const argB = renderFilterArg(b.name, opB.suffix, opB.sampleValue);
	return (
		`Filter example (combined — ${a.name} and ${b.name}): ` +
		`{ ${domainName}(${argA}, ${argB}) { ${selection} } }\n` +
		`Filter note: multiple filters are comma-separated inside the same parenthesis. ` +
		`Any filterable field can be combined this way.\n`
	);
}

/**
 * renderFilterExampleLine formats one type-driven default: a single
 * filter argument with the catalog's sample value, paired with the
 * full selection set so the LLM always sees a complete query.
 */
function renderFilterExampleLine(
	domainName: string,
	f: DomainFieldDef,
	op: FilterOp,
	value: string,
	selection: string,
): string {
	const arg = renderFilterArg(f.name, op.suffix, value);
	return `Filter example (${f.name} ${op.label}): { ${domainName}(${arg}) { ${selection} } }\n`;
}

/**
 * renderFilterExampleOverride formats one author-supplied override.
 * Includes the description as a trailing comment so the embedding
 * model picks up the realistic phrasing.
 */
function renderFilterExampleOverride(
	domainName: string,
	f: DomainFieldDef,
	op: FilterOp,
	ex: ExampleDef,
	value: string,
	selection: string,
): string {
	const arg = renderFilterArg(f.name, op.suffix, value);
	const header = ex.description
		? `Filter example (${f.name} ${op.label}) — ${ex.description}`
		: `Filter example (${f.name} ${op.label})`;
	return `${header}: { ${domainName}(${arg}) { ${selection} } }\n`;
}

// ─── Section: relations ──────────────────────────────────────────────

/** writeRelations renders has_many / has_one / belongs_to lines. */
function writeRelations(parts: string[], def: DomainDef): void {
	if (def.relations.length === 0) return;
	const lines = def.relations.map(describeRelation);
	parts.push(`Relations: ${lines.join(" | ")}\n`);
}

/** describeRelation renders one relation in compact form. */
function describeRelation(r: RelationDef): string {
	return `${r.name} (${r.kind} ${r.target}, ${r.localField} -> ${r.foreignField})`;
}

// ─── Section: meta sources ───────────────────────────────────────────

/**
 * writeMetas emits everything an LLM needs to populate dropdown fields:
 *
 *   1. A "Dropdown sources" summary line listing every meta with its
 *      source table and label column (good for human readers and for
 *      retrieval against German queries like "alle Städte").
 *
 *   2. One ready-to-copy GraphQL meta query per meta, named verbatim
 *      after the meta — `meta_<name>` — so the LLM never has to
 *      construct the query name.
 *
 * Emitting (2) explicitly is the whole point of this block: it turns
 * an open-ended reasoning task into a template-filling task, which
 * smaller models handle reliably.
 */
function writeMetas(parts: string[], def: DomainDef): void {
	if (def.metas.length === 0) return;

	const summary = def.metas.map(describeMetaSource).join(" | ");
	parts.push(`Dropdown sources: ${summary}\n`);

	parts.push("Meta queries (copy verbatim, do not invent names):\n");
	for (const m of def.metas) {
		parts.push(`  - ${m.name}: { ${metaQueryName(m)} { value label } }\n`);
	}
}

/** describeMetaSource renders one meta as `name (from table, label=col)`. */
function describeMetaSource(m: MetaDef): string {
	return `${m.name} (from ${m.table}.${m.valueField}, label=${m.labelField})`;
}

/**
 * metaQueryName returns the canonical GraphQL meta-query name for a
 * Meta. The contract — `meta_<name>` — is fixed: the GraphQL backend
 * exposes meta queries under exactly this prefix, and the chunk
 * teaches the LLM that fact.
 */
function metaQueryName(m: MetaDef): string {
	return `meta_${m.name}`;
}

// ─── Section: dropdown field mapping ─────────────────────────────────

/**
 * writeDropdownFieldMapping emits two blocks that together teach the
 * LLM how to build a single GraphQL request that carries both the
 * record and every dropdown's options.
 *
 * "Dropdown fields" pairs each field that has an optionsRef with its
 * meta query name, so the mapping is unambiguous even when field and
 * meta don't share a name (e.g. field "role" → meta "roles" → query
 * "meta_roles").
 *
 * "Full example combined query" stitches the main domain query with
 * every meta query into a single brace-block. That's the exact shape
 * the LLM should emit when asked for a record that has dropdowns.
 */
function writeDropdownFieldMapping(parts: string[], def: DomainDef): void {
	const pairs = collectDropdownPairs(def);
	if (pairs.length === 0) return;

	parts.push(
		"Dropdown fields (every one below must be fetched together with its meta):\n",
	);
	for (const p of pairs) {
		parts.push(`  - ${p.field} -> ${p.query}\n`);
	}

	const recordSelection = def.fields.map((f) => f.name).join(" ");
	if (!recordSelection) return;

	// Deduplicate meta blocks: multiple fields can share a meta and
	// the combined query should only fetch each meta once.
	const seen = new Set<string>();
	const metaBlocks: string[] = [];
	for (const p of pairs) {
		if (seen.has(p.query)) continue;
		seen.add(p.query);
		metaBlocks.push(`${p.query} { value label }`);
	}

	parts.push(
		`Full example combined query: { ${def.name} { ${recordSelection} } ${metaBlocks.join(" ")} }\n`,
	);
}

/** Internal pair: the field name and the meta query it sources from. */
interface DropdownPair {
	field: string;
	query: string;
}

/**
 * collectDropdownPairs walks the domain's fields and returns one pair
 * per field whose optionsRef resolves to a declared meta. Fields with
 * a dangling optionsRef are dropped silently — a missing meta is a
 * seed problem, not a chunk problem.
 */
function collectDropdownPairs(def: DomainDef): DropdownPair[] {
	const queryByName = new Map<string, string>();
	for (const m of def.metas) {
		queryByName.set(m.name, metaQueryName(m));
	}

	const pairs: DropdownPair[] = [];
	for (const f of def.fields) {
		if (!f.optionsRef) continue;
		const q = queryByName.get(f.optionsRef);
		if (!q) continue;
		pairs.push({ field: f.name, query: q });
	}
	return pairs;
}

// ─── Section: permissions ────────────────────────────────────────────

/** writePermissions renders the role-based permission matrix. */
function writePermissions(parts: string[], def: DomainDef): void {
	if (def.permissions.length === 0) return;
	const lines = def.permissions.map(describePermission);
	parts.push(`Permissions: ${lines.join(" | ")}\n`);
}

/** describePermission renders one role's actions as `role=read,write,...`. */
function describePermission(p: PermissionDef): string {
	return `${p.role}=${p.actions.join(",")}`;
}

// ─── Section: AI hints ───────────────────────────────────────────────

/**
 * writeAiHints renders human-authored AI hints — behaviour rules,
 * format hints, caveats. Empty bodies are skipped silently so an
 * accidental empty block doesn't pollute the chunk.
 */
function writeAiHints(parts: string[], def: DomainDef): void {
	const usable = def.aiHints.filter((h) => collapseWhitespace(h.body) !== "");
	if (usable.length === 0) return;

	parts.push("AI hints:\n");
	for (const h of usable) {
		parts.push(`  - ${h.name}: ${collapseWhitespace(h.body)}\n`);
	}
}

/**
 * collapseWhitespace normalises runs of whitespace (newlines, tabs,
 * multiple spaces) into single spaces. Authors write multi-line AI
 * hints for readability; the embedding model wants flat text.
 */
function collapseWhitespace(s: string): string {
	return s.replace(/\s+/g, " ").trim();
}

// Re-export for tests / external consumers that want to inspect a
// single field's filter examples without rendering the whole chunk.
export type { FieldTypeDef, AiHintDef };
