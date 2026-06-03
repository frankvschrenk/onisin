// renderer/view-chunk.ts — ViewDef → LLM-friendly text chunk.
//
// Companion to renderLLMChunk for domains. Where the domain chunk is
// fully derived from the DomainDef, the view chunk wraps a small
// structured header around the verbatim DSL source — the source
// itself is more compact and more author-faithful than anything we
// could re-render. The LLM benefits from both: the header gives
// retrieval-friendly cues (name, title, target domains, default
// flag), the source carries the exact field list, layout and any
// AI hints the author wrote.
//
// This used to live inside `apps/oosai/src/sources.ts`, where the
// pipeline embedded the result into pgvector. Hoisted here so the
// editor (oosd) can call it without round-tripping through oosai —
// the renderer is a pure function and belongs next to the parser
// and mapper that produce its input.

import type { ViewDef } from "../types";

/**
 * renderViewChunk produces the structured chunk text used both for
 * embedding into pgvector and for editor-side debug previews.
 *
 * The shape is intentionally minimal:
 *
 *   View: <name> "<title>" over <domain>[(alias)][, ...] [(default)]
 *
 *   <verbatim DSL source>
 *
 * Stability matters: the embedding pipeline hashes the chunk to
 * detect "no change, skip re-embed", so the byte layout is part of
 * the contract.
 */
export function renderViewChunk(def: ViewDef, source: string): string {
	const header = renderViewHeader(def);
	return `${header}\n\n${source}`;
}

/**
 * renderViewHeader builds the one-line identity header that prefixes
 * the source. Aliases are inlined only when they differ from the
 * domain name so single-domain views keep the compact "over person"
 * form; multi-domain views render every participant.
 */
function renderViewHeader(def: ViewDef): string {
	const flags = def.default ? " (default)" : "";
	const overList = def.domains
		.map((d) => (d.alias === d.name ? d.name : `${d.name}(${d.alias})`))
		.join(", ");
	return `View: ${def.name} "${def.title}" over ${overList}${flags}`;
}
