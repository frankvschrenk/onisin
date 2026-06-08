// naming.ts — Naming conventions for GraphQL identifiers.
//
// Centralised so the schema builder, the resolvers and the LLM-chunk
// renderer agree byte-for-byte on what names the schema exposes.
// A drift between any two of them would mean queries that the LLM
// generates fail at runtime — exactly the failure mode the chunk
// renderer was built to prevent.

/**
 * domainQueryName returns the top-level Query field name for a
 * domain. Convention: lowercase, identical to the DSL domain name.
 *
 *   domain person { ... }   →  Query.person(...)
 */
export function domainQueryName(domainName: string): string {
	return domainName;
}

/**
 * domainTypeName returns the GraphQL object type name for a domain.
 * Convention: PascalCase. Same domain rendered as `person` for the
 * field, `Person` for the type.
 */
export function domainTypeName(domainName: string): string {
	if (domainName.length === 0) return domainName;
	// Uppercase first char, then preserve underscores by upper-casing
	// the char after each — `notify_channel` → `NotifyChannel`.
	return domainName
		.split("_")
		.map((part) => (part ? part[0]!.toUpperCase() + part.slice(1) : part))
		.join("");
}

/**
 * mutationFieldName returns the Mutation field name for a (domain,
 * verb) pair. Convention: `<verb>_<domain>` to match the legacy
 * Go renderer's wire format and what the LLM-chunk renderer teaches
 * downstream models.
 */
export function mutationFieldName(
	verb: "update" | "insert" | "delete",
	domainName: string,
): string {
	return `${verb}_${domainName}`;
}

/**
 * metaQueryName returns the GraphQL Query field name for a Meta.
 * Convention: `meta_<name>`. Identical to what the LLM-chunk renderer
 * emits — see `renderer/llm-chunk.ts` in oos-dsls-ts.
 */
export function metaQueryName(metaName: string): string {
	return `meta_${metaName}`;
}

/**
 * metaTypeName returns the GraphQL object type name for a Meta.
 * Convention: `MetaOption_<name>`. Always exposes `value` and `label`
 * regardless of the source columns — the Meta resolver aliases.
 */
export function metaTypeName(metaName: string): string {
	return `MetaOption_${metaName}`;
}
