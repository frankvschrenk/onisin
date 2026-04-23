package store

// schema_chunk.go — ContextAst → text chunk for embedding.
//
// Takes one dsl.ContextAst and renders a structured plain-text description
// designed for LLM consumption: small embedding models must be able to match
// it against natural-language queries, and generation models must be able to
// read it as a manual for building correct GraphQL queries against the
// described context.
//
// Information carried into the chunk:
//
//  1. Structural facts from the AST (name, kind, source, fields, relations,
//     permissions, navigate targets, actions, meta references).
//  2. Filter-example stanzas generated automatically from field type + the
//     filterable flag — so every filterable field gets working examples
//     regardless of its name.
//  3. Per-field Examples overrides carried on FieldAst, which give realistic
//     values and short explanatory comments.
//  4. Dropdown lookup sources with the concrete meta_* GraphQL queries to
//     fetch their options, and a ready-to-use combined query example so
//     smaller LLMs don't have to synthesise the shape from scratch.
//  5. AI hints declared via context-scoped prompts.
//
// The chunk layout follows a fixed order so that embeddings land in
// consistent parts of the vector space across contexts.
//
// Design note: the chunk is rendered from the already-built AST instead of
// re-parsing the CTX XML. This removes the second parser (and with it the
// risk of divergence between the chunk view and the GraphQL view of the
// same context) and ensures that any future enrichment of the AST is
// automatically available to the chunk too.

import (
	"fmt"
	"strings"

	"onisin.com/oos-common/dsl"
)

// ── Public types ──────────────────────────────────────────────────────────────

// SchemaChunk holds one context description ready for embedding.
type SchemaChunk struct {
	ContextName string `json:"context_name"`
	Text        string `json:"chunk"`
}

// dropdownPair binds one dropdown field to the GraphQL meta query that
// supplies its options. Kept at package level so helpers can share the
// type cleanly.
type dropdownPair struct {
	field string
	query string
}

// ── Entry point ───────────────────────────────────────────────────────────────

// BuildSchemaChunks renders one SchemaChunk per ContextAst.
//
// Operational metadata that does not describe a context (global prompts,
// groups, DSN wiring) is already filtered out by dsl.BuildAST and therefore
// never reaches this function. No need to skip anything here.
func BuildSchemaChunks(contexts []dsl.ContextAst) []SchemaChunk {
	if len(contexts) == 0 {
		return nil
	}
	chunks := make([]SchemaChunk, 0, len(contexts))
	for i := range contexts {
		chunks = append(chunks, SchemaChunk{
			ContextName: contexts[i].Name,
			Text:        renderChunk(&contexts[i]),
		})
	}
	return chunks
}

// ── Chunk rendering ───────────────────────────────────────────────────────────

// renderChunk produces a structured, human-readable description of a context.
// Section order is stable across contexts so embeddings compare cleanly.
func renderChunk(ctx *dsl.ContextAst) string {
	var sb strings.Builder

	writeHeader(&sb, ctx)
	writeAliases(&sb, ctx)
	writeFields(&sb, ctx)
	writeListFieldsAndQuery(&sb, ctx)
	writeFilterExamples(&sb, ctx)
	writeRelations(&sb, ctx)
	writeNavigates(&sb, ctx)
	writeActions(&sb, ctx)
	writeMetas(&sb, ctx)
	writePermissions(&sb, ctx)
	writeAIPrompts(&sb, ctx)

	return sb.String()
}

// writeHeader emits the one-line identity of the context.
func writeHeader(sb *strings.Builder, ctx *dsl.ContextAst) {
	fmt.Fprintf(sb, "Context: %s (%s)\n", ctx.Name, ctx.Kind)
	fmt.Fprintf(sb, "Source: %s\n", ctx.Source)
}

// writeAliases emits German alias lines that boost retrieval for queries
// phrased in German. Generic — derived from the context name suffix.
func writeAliases(sb *strings.Builder, ctx *dsl.ContextAst) {
	switch {
	case strings.HasSuffix(ctx.Name, "_list"):
		base := strings.TrimSuffix(ctx.Name, "_list")
		fmt.Fprintf(sb, "Alias: %s Liste, alle %s, %s anzeigen\n", base, base, base)
	case strings.HasSuffix(ctx.Name, "_detail"):
		base := strings.TrimSuffix(ctx.Name, "_detail")
		fmt.Fprintf(sb, "Alias: %s Detail, %s bearbeiten, %s\n", base, base, base)
	}
}

// writeFields emits the full field listing for entity contexts. Collection
// contexts use list_fields instead — the full list would just bloat the chunk.
func writeFields(sb *strings.Builder, ctx *dsl.ContextAst) {
	if ctx.Kind == "collection" || len(ctx.Fields) == 0 {
		return
	}
	parts := make([]string, 0, len(ctx.Fields))
	for _, f := range ctx.Fields {
		parts = append(parts, describeField(f))
	}
	fmt.Fprintf(sb, "Fields: %s\n", strings.Join(parts, " | "))
}

// describeField renders one field as `name (type, attr1, attr2, ...)`.
func describeField(f dsl.FieldAst) string {
	var attrs []string
	if f.Readonly {
		attrs = append(attrs, "readonly")
	}
	if f.Filterable {
		attrs = append(attrs, "filterable")
	}
	if f.MetaRef != "" {
		attrs = append(attrs, "meta="+f.MetaRef)
	}
	if len(attrs) == 0 {
		return fmt.Sprintf("%s (%s)", f.Name, f.Type)
	}
	return fmt.Sprintf("%s (%s, %s)", f.Name, f.Type, strings.Join(attrs, ", "))
}

// writeListFieldsAndQuery emits the ALLOWED query fields block and a canonical
// "fetch all" GraphQL query. Only meaningful for collection contexts.
func writeListFieldsAndQuery(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.ListFields) == 0 {
		return
	}
	lf := strings.Join(ctx.ListFields, ", ")
	// In the rendered query the fields are space-separated (GraphQL selection set),
	// while the ALLOWED block stays comma-separated for readability.
	lfQuery := strings.Join(ctx.ListFields, " ")
	fmt.Fprintf(sb, "ALLOWED query fields (ONLY these, no others): %s\n", lf)
	fmt.Fprintf(sb, "GraphQL query all: { %s { %s } }\n", ctx.Name, lfQuery)
}

// writeFilterExamples emits filter examples for every filterable field.
// Generation is type-driven: the operators supported by the GraphQL backend
// for each type are enumerated, with per-field overrides when given.
//
// If no list_fields is declared (entity context) no filter examples make sense,
// since entity contexts are typically fetched by id.
func writeFilterExamples(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.ListFields) == 0 {
		return
	}

	filterable := collectFilterable(ctx.Fields)
	if len(filterable) == 0 {
		return
	}

	names := make([]string, 0, len(filterable))
	for _, f := range filterable {
		names = append(names, f.Name)
	}
	fmt.Fprintf(sb, "Filterable fields: %s\n", strings.Join(names, ", "))

	lfQuery := strings.Join(ctx.ListFields, " ")
	for _, f := range filterable {
		for _, ex := range filterExamplesForField(ctx.Name, lfQuery, f) {
			sb.WriteString(ex)
		}
	}
}

// collectFilterable returns the subset of fields marked filterable.
func collectFilterable(fields []dsl.FieldAst) []dsl.FieldAst {
	out := make([]dsl.FieldAst, 0, len(fields))
	for _, f := range fields {
		if f.Filterable {
			out = append(out, f)
		}
	}
	return out
}

// filterExamplesForField produces the per-field filter-example lines.
//
// Strategy:
//  1. For each operator supported by the field's type, emit one line with a
//     sensible default value. These are the syntax-teaching examples.
//  2. On top, emit every Example carried on the field — those give realistic
//     values and context.
//
// Both are always emitted together: the typed defaults ensure the LLM knows
// the syntax exists; the overrides make the chunk feel specific and accurate.
func filterExamplesForField(contextName, listFields string, f dsl.FieldAst) []string {
	var lines []string

	for _, op := range operatorsForType(f.Type) {
		line := fmt.Sprintf("Filter example (%s %s): { %s(%s) { %s } }\n",
			f.Name, op.label,
			contextName,
			renderFilterArg(f.Name, op.suffix, op.sampleValue),
			listFields,
		)
		lines = append(lines, line)
	}

	for _, ex := range f.Examples {
		op := findOperator(f.Type, ex.Op)
		if op == nil {
			continue // unknown operator for this type — skip silently
		}
		header := fmt.Sprintf("Filter example (%s %s)", f.Name, op.label)
		if ex.Comment != "" {
			header += " — " + ex.Comment
		}
		line := fmt.Sprintf("%s: { %s(%s) { %s } }\n",
			header,
			contextName,
			renderFilterArg(f.Name, op.suffix, formatExampleValue(f.Type, ex.Value)),
			listFields,
		)
		lines = append(lines, line)
	}

	return lines
}

// filterOp describes one GraphQL filter operator for a given field type.
type filterOp struct {
	op          string // eq, like, gt, lt
	label       string // human-readable label for the chunk
	suffix      string // GraphQL argument suffix ("", "_like", "_gt", "_lt")
	sampleValue string // default value used when no override is given
}

// operatorsForType returns the operators we generate filter examples for,
// given a field's declared type. The sample values aim to be realistic
// enough that the LLM picks up idiomatic shapes (strings quoted, numbers bare).
func operatorsForType(fieldType string) []filterOp {
	switch fieldType {
	case "string", "text":
		return []filterOp{
			{op: "eq", label: "exact", suffix: "", sampleValue: `"value"`},
			{op: "like", label: "contains", suffix: "_like", sampleValue: `"value"`},
		}
	case "int":
		return []filterOp{
			{op: "eq", label: "exact", suffix: "", sampleValue: "42"},
			{op: "gt", label: ">", suffix: "_gt", sampleValue: "0"},
			{op: "lt", label: "<", suffix: "_lt", sampleValue: "100"},
		}
	case "float":
		return []filterOp{
			{op: "eq", label: "exact", suffix: "", sampleValue: "0.0"},
			{op: "gt", label: ">", suffix: "_gt", sampleValue: "0.0"},
			{op: "lt", label: "<", suffix: "_lt", sampleValue: "100.0"},
		}
	case "bool":
		return []filterOp{
			{op: "eq", label: "is", suffix: "", sampleValue: "true"},
		}
	case "date", "datetime":
		return []filterOp{
			{op: "gt", label: "after", suffix: "_gt", sampleValue: `"2024-01-01"`},
			{op: "lt", label: "before", suffix: "_lt", sampleValue: `"2024-12-31"`},
		}
	}
	return nil
}

// findOperator returns the filterOp matching the user-supplied op name
// for the given field type, or nil if it's not a valid combination.
func findOperator(fieldType, opName string) *filterOp {
	for _, op := range operatorsForType(fieldType) {
		if op.op == opName {
			return &op
		}
	}
	return nil
}

// renderFilterArg formats a single filter argument.
// Callers pass the value pre-formatted (quoted for strings/dates, bare for
// numbers/bools). We just concatenate field name, suffix and value.
func renderFilterArg(fieldName, suffix, value string) string {
	return fmt.Sprintf("%s%s: %s", fieldName, suffix, value)
}

// formatExampleValue quotes or leaves bare a user-supplied override value
// based on the declared field type, matching the convention the typed
// sample values already follow.
//
// String, text and date-like values are wrapped in double quotes so the
// resulting GraphQL snippet is syntactically correct. Numbers and booleans
// pass through unchanged — GraphQL expects them bare.
func formatExampleValue(fieldType, value string) string {
	switch fieldType {
	case "string", "text", "date", "datetime":
		return fmt.Sprintf(`"%s"`, value)
	default:
		return value
	}
}

// writeRelations renders the relation block (has_many, has_one, belongs_to).
func writeRelations(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Relations) == 0 {
		return
	}
	parts := make([]string, 0, len(ctx.Relations))
	for _, r := range ctx.Relations {
		bind := fmt.Sprintf("%s -> %s", r.BindLocal, r.BindForeign)
		parts = append(parts, fmt.Sprintf("%s → %s (%s, %s)",
			r.Name, r.ToContext, r.Type, bind))
	}
	fmt.Fprintf(sb, "Relations: %s\n", strings.Join(parts, " | "))
}

// writeNavigates documents which events open which other contexts.
// Helps the LLM understand Board navigation when orchestrating UI flows.
func writeNavigates(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Navigates) == 0 {
		return
	}
	parts := make([]string, 0, len(ctx.Navigates))
	for _, n := range ctx.Navigates {
		if n.BindLocal != "" || n.BindForeign != "" {
			bind := fmt.Sprintf("%s -> %s", n.BindLocal, n.BindForeign)
			parts = append(parts, fmt.Sprintf("%s → %s (%s)", n.Event, n.ToContext, bind))
		} else {
			parts = append(parts, fmt.Sprintf("%s → %s", n.Event, n.ToContext))
		}
	}
	fmt.Fprintf(sb, "Navigation: %s\n", strings.Join(parts, " | "))
}

// writeActions lists the save/delete actions defined on the context.
func writeActions(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Actions) == 0 {
		return
	}
	parts := make([]string, 0, len(ctx.Actions))
	for _, a := range ctx.Actions {
		if a.Confirm != "" {
			parts = append(parts, fmt.Sprintf("%s (%s, confirm)", a.Event, a.Type))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", a.Event, a.Type))
		}
	}
	fmt.Fprintf(sb, "Actions: %s\n", strings.Join(parts, " | "))
}

// writeMetas emits everything an LLM needs to populate dropdown fields.
//
// There are three independent things a small LLM has to know, and the
// chunk used to carry at most the first one:
//
//  1. Which lookup sources exist (meta name + table/value/label).
//  2. Which concrete GraphQL query fetches each lookup's options
//     (meta_<n> { value label }) — so the LLM doesn't have to guess
//     the query name.
//  3. Which field needs which meta, and a fully-formed combined query
//     that fetches the record plus every dropdown's options in one shot.
//
// Emitting (2) and (3) explicitly is the whole point of this block: they
// turn an open-ended reasoning task into a template-filling task, which
// smaller models handle reliably.
func writeMetas(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Metas) == 0 {
		return
	}

	// (1) Source summary — kept for humans reading the chunk.
	parts := make([]string, 0, len(ctx.Metas))
	for _, m := range ctx.Metas {
		parts = append(parts, fmt.Sprintf("%s (from %s.%s, label=%s)",
			m.Name, m.Table, m.Value, m.Label))
	}
	fmt.Fprintf(sb, "Dropdown sources: %s\n", strings.Join(parts, " | "))

	// (2) One GraphQL query per meta. GQLQuery is always "meta_<n>"
	// (set in dsl.buildContext) — we render it verbatim so the LLM can
	// copy it without constructing the name.
	sb.WriteString("Meta queries (copy verbatim, do not invent names):\n")
	for _, m := range ctx.Metas {
		fmt.Fprintf(sb, "  - %s: { %s { value label } }\n", m.Name, m.GQLQuery)
	}

	// (3) Field → meta pairing and a combined query.
	writeDropdownFieldMapping(sb, ctx)
}

// writeDropdownFieldMapping emits two blocks that together teach the LLM
// how to build a single GraphQL request that carries both the record
// and every dropdown's options — the shape oos expects for an entity
// screen to render correctly.
//
// "Dropdown fields" pairs each field that has a MetaRef with its meta
// query name, so the mapping is unambiguous even when field and meta
// don't share a name (e.g. field "role" → meta "roles" → query
// "meta_roles").
//
// "Full example combined query" stitches the main context query with
// every meta query into a single brace-block. That's the exact shape
// the LLM should emit when asked for an entity record that has
// dropdowns.
func writeDropdownFieldMapping(sb *strings.Builder, ctx *dsl.ContextAst) {
	pairs := collectDropdownPairs(ctx)
	if len(pairs) == 0 {
		return
	}

	sb.WriteString("Dropdown fields (every one below must be fetched together with its meta):\n")
	for _, p := range pairs {
		fmt.Fprintf(sb, "  - %s -> %s\n", p.field, p.query)
	}

	recordFields := combinedQueryRecordFields(ctx, pairs)
	if recordFields == "" {
		return
	}

	// Assemble the combined query. Every meta query appears at most once
	// even if multiple fields share the same meta.
	var metaBlocks []string
	seen := make(map[string]bool)
	for _, p := range pairs {
		if seen[p.query] {
			continue
		}
		seen[p.query] = true
		metaBlocks = append(metaBlocks, fmt.Sprintf("%s { value label }", p.query))
	}

	idArg := ""
	if ctx.Kind == "entity" {
		idArg = "(id: 1)"
	}
	fmt.Fprintf(sb,
		"Full example combined query: { %s%s { %s } %s }\n",
		ctx.Name, idArg, recordFields, strings.Join(metaBlocks, " "),
	)
}

// collectDropdownPairs walks the context's fields and returns one pair per
// field that carries a MetaRef whose target is declared on the context.
// Fields without a MetaRef, or with a MetaRef that doesn't resolve, are
// dropped silently — a missing meta is a seed problem, not a chunk problem.
func collectDropdownPairs(ctx *dsl.ContextAst) []dropdownPair {
	metaByName := make(map[string]string, len(ctx.Metas))
	for _, m := range ctx.Metas {
		metaByName[m.Name] = m.GQLQuery
	}

	pairs := make([]dropdownPair, 0, len(ctx.Fields))
	for _, f := range ctx.Fields {
		if f.MetaRef == "" {
			continue
		}
		q, ok := metaByName[f.MetaRef]
		if !ok {
			continue
		}
		pairs = append(pairs, dropdownPair{field: f.Name, query: q})
	}
	return pairs
}

// combinedQueryRecordFields picks the record-side selection set for the
// combined example query.
//
// Collections have an explicit list_fields declaration — that's what the
// screen shows and what we hand verbatim to the LLM.
//
// Entity contexts have no list_fields: the detail screen needs every field
// to render correctly, so we emit every declared field. Showing only id +
// dropdowns here would teach the LLM a partial query shape that it would
// then copy for real detail loads — exactly the failure mode we're trying
// to avoid.
//
// Pairs is kept in the signature because the fallback path for contexts
// without declared fields (unusual, but possible during bootstrap) still
// benefits from at least listing the dropdown fields; see the tail below.
func combinedQueryRecordFields(ctx *dsl.ContextAst, pairs []dropdownPair) string {
	if len(ctx.ListFields) > 0 {
		return strings.Join(ctx.ListFields, " ")
	}

	if len(ctx.Fields) > 0 {
		names := make([]string, 0, len(ctx.Fields))
		for _, f := range ctx.Fields {
			names = append(names, f.Name)
		}
		return strings.Join(names, " ")
	}

	// Bootstrap / empty-context fallback: id + each dropdown field. We keep
	// this path because a context with metas but no fields is weird but
	// not catastrophic, and returning an empty record block would break
	// the combined query's syntax.
	names := []string{"id"}
	seen := map[string]bool{"id": true}
	for _, p := range pairs {
		if seen[p.field] {
			continue
		}
		seen[p.field] = true
		names = append(names, p.field)
	}
	return strings.Join(names, " ")
}

// writePermissions renders the role-based permission matrix.
// Permissions carry Action slices; join them with comma for a compact line.
func writePermissions(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Permissions) == 0 {
		return
	}
	parts := make([]string, 0, len(ctx.Permissions))
	for _, p := range ctx.Permissions {
		actions := joinActions(p.Actions)
		parts = append(parts, fmt.Sprintf("%s=%s", p.Role, actions))
	}
	fmt.Fprintf(sb, "Permissions: %s\n", strings.Join(parts, " | "))
}

// joinActions converts []dsl.Action into a comma-joined string.
func joinActions(actions []dsl.Action) string {
	s := make([]string, 0, len(actions))
	for _, a := range actions {
		s = append(s, string(a))
	}
	return strings.Join(s, ",")
}

// writeAIPrompts renders context-scoped AI hints. These are human-authored
// instructions tuned for the LLM — behaviour rules, format hints, caveats.
func writeAIPrompts(sb *strings.Builder, ctx *dsl.ContextAst) {
	if len(ctx.Prompts) == 0 {
		return
	}
	sb.WriteString("AI hints:\n")
	for _, p := range ctx.Prompts {
		text := collapseWhitespace(p.Text)
		if text == "" {
			continue
		}
		fmt.Fprintf(sb, "  - %s: %s\n", p.Name, text)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// collapseWhitespace turns runs of whitespace in authored prompt text into
// single spaces and trims the result — so multi-line authoring stays
// readable while the chunk remains compact.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
