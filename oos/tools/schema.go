package tools

// schema.go — OOS schema loading and injection for AI context.
//
// Three strategies controlled by llm.SchemaStrategy:
//
//   compact — inject a one-line summary per context into the prompt;
//             oos_schema_search is still available for GraphQL details.
//             Scales to hundreds of contexts (~50 tokens each).
//
//   full    — embed all schema chunks directly in the prompt.
//             Best for large models (vLLM cluster) with big context windows.
//             Use when the total chunk count fits comfortably in the window.
//
//   rag     — no schema in the prompt at all; the model must call
//             oos_schema_search before every query. Minimal token usage
//             but requires one extra tool-call step per request.

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"onisin.com/oos-common/dsl"
	"onisin.com/oos-common/llm"
	"onisin.com/oos/helper"
)

// SchemaChunk is a single context description returned by SchemaSearch.
type SchemaChunk struct {
	ContextName string `json:"context_name"`
	Chunk       string `json:"chunk"`
}

// SchemaSearch queries oos.oos_schema via OOSP for the n chunks most
// relevant to query. Returns the combined chunk text ready for prompt injection.
func SchemaSearch(query string, n int) (string, error) {
	if helper.OOSP == nil {
		return "", fmt.Errorf("OOSP not connected")
	}
	if n <= 0 {
		n = 3
	}

	raw, err := helper.OOSP.Post("/schema/search", map[string]any{
		"query": query,
		"n":     n,
	})
	if err != nil {
		return "", fmt.Errorf("schema search: %w", err)
	}

	var chunks []SchemaChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		log.Printf("[tools] schema search parse error: %v — raw: %.200s", err, raw)
		return raw, nil
	}

	if len(chunks) == 0 {
		return "(no schema chunks found)", nil
	}

	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(c.Chunk)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// schemaCache holds the pre-built schema prompt so it is computed only once
// per application run rather than on every session open.
var schemaCache struct {
	prompt string
	loaded bool
}

// BuildSchemaPrompt returns the schema section of the system prompt
// according to the configured strategy (compact / full / rag).
// The result is cached after the first call.
func BuildSchemaPrompt() string {
	if schemaCache.loaded {
		return schemaCache.prompt
	}
	switch llm.SchemaStrategy {
	case "full":
		schemaCache.prompt = buildFullSchema()
	case "rag":
		schemaCache.prompt = buildRAGHint()
	default: // "compact"
		schemaCache.prompt = buildCompactSchema()
	}
	schemaCache.loaded = true
	return schemaCache.prompt
}

// InvalidateSchemaCache clears the cached schema prompt so it is reloaded
// on the next session open. Call when oos.ctx changes at runtime.
func InvalidateSchemaCache() {
	schemaCache.loaded = false
	schemaCache.prompt = ""
}

// buildCompactSchema fetches all chunks and renders only the essential
// GraphQL query per context. This gives the model enough to call oos_query
// directly without a schema search step, while keeping the prompt small.
func buildCompactSchema() string {
	if helper.OOSP == nil {
		return "(schema not available — OOSP not connected)"
	}

	chunks, err := fetchAllChunks()
	if err != nil || len(chunks) == 0 {
		log.Printf("[tools] compact schema fetch: %v", err)
		return "(schema not available)"
	}

	var sb strings.Builder
	sb.WriteString("## OOS SCHEMA\n\n")
	sb.WriteString("Use ONLY the fields and filters shown. Do not invent field names.\n\n")

	for _, c := range chunks {
		// Extract only the GraphQL query line and filter examples from the chunk.
		for _, line := range strings.Split(c.Chunk, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "GraphQL query all:") ||
				strings.HasPrefix(line, "Filter example") ||
				strings.HasPrefix(line, "Context:") {
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildFullSchema fetches all chunks and embeds them completely.
// For large models where context window is not a concern.
func buildFullSchema() string {
	if helper.OOSP == nil {
		return "(schema not available — OOSP not connected)"
	}

	chunks, err := fetchAllChunks()
	if err != nil || len(chunks) == 0 {
		log.Printf("[tools] full schema fetch: %v", err)
		return "(schema not available)"
	}

	var sb strings.Builder
	sb.WriteString("## OOS SCHEMA\n\n")
	for _, c := range chunks {
		sb.WriteString(c.Chunk)
		sb.WriteString("\n---\n")
	}
	return sb.String()
}

// fetchAllChunks retrieves all schema chunks from OOSP without embedding.
func fetchAllChunks() ([]SchemaChunk, error) {
	raw, err := helper.OOSP.Get("/schema/all")
	if err != nil {
		return nil, fmt.Errorf("schema/all: %w", err)
	}
	var chunks []SchemaChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		return nil, fmt.Errorf("schema/all parse: %w", err)
	}
	return chunks, nil
}

// buildRAGHint returns a minimal hint that tells the model to use the search tool.
// No schema is embedded — the model must call oos_schema_search every time.
func buildRAGHint() string {
	return "## OOS SCHEMA\n\nUse the oos_schema_search tool to discover available contexts, fields and GraphQL examples before calling oos_query.\n"
}

// extractSummary returns the first non-empty, non-alias line from a chunk.
// Used to build the compact one-liner per context.
func extractSummary(chunk string) string {
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the Context: header line — use the next meaningful line.
		if strings.HasPrefix(line, "Context:") {
			continue
		}
		if strings.HasPrefix(line, "Alias:") {
			continue
		}
		// Return Source / GraphQL / Fields line as summary.
		return line
	}
	return ""
}

// SystemContext returns the User section of the system prompt.
//
// The section carries two pieces of information the model needs for every
// permission-related decision:
//
//  1. Who the user is and which role they hold.
//  2. A per-context table of allowed actions (read / write / delete) under
//     that role.
//
// Without (2) the model has to read the Permissions line of each context
// chunk and cross-reference it against the role — a small but reliable
// source of mistakes for smaller models. Rendering the resolved matrix
// here turns it into a lookup.
//
// When no role is active (pre-login or AST missing) we return an empty
// string; the caller drops the User section entirely.
func SystemContext() string {
	if helper.ActiveRole == "" || helper.ActiveUsername() == "" {
		// Fallback: still show username if present, no role → no permission table.
		if helper.ActiveUsername() != "" {
			return fmt.Sprintf("## User\n\nYou are assisting %s.\n\n", helper.ActiveUsername())
		}
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## User\n\nYou are assisting %s (role: %s).\n\n",
		helper.ActiveUsername(), helper.ActiveRole)

	matrix := renderPermissionMatrix(helper.ActiveRole)
	if matrix != "" {
		sb.WriteString("### What this role may do per context\n\n")
		sb.WriteString(matrix)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderPermissionMatrix returns a bullet list of `context: actions` for
// every context in the AST that declares permissions, resolved for the
// given role.
//
// Contexts without declared permissions are treated as unrestricted and
// listed with "any" so the model doesn't mistake absence of data for a
// deny. Context names are sorted alphabetically — stable order makes the
// prompt diffable and comparable between sessions.
func renderPermissionMatrix(role string) string {
	if helper.OOSAst == nil || len(helper.OOSAst.Contexts) == 0 {
		return ""
	}

	names := make([]string, 0, len(helper.OOSAst.Contexts))
	actionsByCtx := make(map[string]string, len(helper.OOSAst.Contexts))

	for i := range helper.OOSAst.Contexts {
		ctx := &helper.OOSAst.Contexts[i]
		names = append(names, ctx.Name)
		actionsByCtx[ctx.Name] = resolveActionsForRole(ctx, role)
	}
	sort.Strings(names)

	// Pad context names to the longest one so the list lines up; easier
	// for a small model to parse visually.
	maxLen := 0
	for _, n := range names {
		if len(n) > maxLen {
			maxLen = len(n)
		}
	}

	var sb strings.Builder
	for _, n := range names {
		padded := n + strings.Repeat(" ", maxLen-len(n))
		fmt.Fprintf(&sb, "- %s  %s\n", padded, actionsByCtx[n])
	}
	return sb.String()
}

// resolveActionsForRole returns a comma-joined list of actions the given
// role has on ctx, or "none" if the role is declared but has no actions,
// or "any" when no permissions are declared on the context at all.
//
// The "any" case matches ContextAst.IsAllowed semantics: an undeclared
// permission set means "no restriction". We surface that explicitly in
// the prompt so the model doesn't assume missing data equals denial.
func resolveActionsForRole(ctx *dsl.ContextAst, role string) string {
	actions, hasPerms := ctx.AllowedActions(role)
	if !hasPerms {
		return "any (no permissions declared)"
	}
	if len(actions) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		parts = append(parts, string(a))
	}
	return strings.Join(parts, ", ")
}

// BuildGlobalPromptsSection renders the ai/prompt blocks from global.conf.xml
// as a markdown section for the system prompt.
//
// These prompts are authored by whoever maintains the CTX files — not by
// the client code — so admins can change LLM behaviour (language, query
// style, mutation workflow) without a client rebuild. When the block is
// empty (no global.conf.xml, or no <prompt> entries in it) the caller
// should keep whatever fallback it had.
//
// The output is a markdown section with each prompt under its own heading:
//
//	## Instructions
//
//	### system
//	<prompt text>
//
//	### query_behavior
//	<prompt text>
//
// Sub-headings make it easy for the model to address individual rules
// (e.g. "per query_behavior I must use list_fields") while keeping the
// block structured enough to read.
func BuildGlobalPromptsSection() string {
	if helper.OOSAst == nil || len(helper.OOSAst.Prompts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Instructions\n\n")
	for _, p := range helper.OOSAst.Prompts {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		name := p.Name
		if name == "" {
			name = "general"
		}
		fmt.Fprintf(&sb, "### %s\n%s\n\n", name, text)
	}
	return sb.String()
}

// roleAllows reports whether the active role is allowed to perform action
// on contextName. It is the single entry point used by both tool handlers
// (to short-circuit a disallowed call) and the prompt builder (to render
// the permission matrix).
//
// If the role is empty, the AST is missing, or the context has no declared
// permissions, we err on the side of permitting the action — matching
// ContextAst.IsAllowed's own semantics. Hardening this to deny-by-default
// belongs on the server side, not here, because the client can always be
// bypassed.
func roleAllows(contextName string, action dsl.Action) bool {
	if helper.ActiveRole == "" || helper.OOSAst == nil {
		return true
	}
	for i := range helper.OOSAst.Contexts {
		c := &helper.OOSAst.Contexts[i]
		if c.Name == contextName {
			return c.IsAllowed(helper.ActiveRole, action)
		}
	}
	return true
}

// hasPermission is kept as a thin alias for the existing Delete check so
// the ui.go call site doesn't need to change. New code should use
// roleAllows directly for clarity.
func hasPermission(contextName string) bool {
	return roleAllows(contextName, dsl.ActionDelete)
}

// LoadAST fetches the AST from OOSP and returns it as formatted JSON.
// Only used for diagnostics — the AI uses schema search instead.
func LoadAST() string {
	for i := 0; i < 20 && helper.OOSP == nil; i++ {
		time.Sleep(500 * time.Millisecond)
	}

	if helper.OOSP != nil {
		ast, role, err := helper.OOSP.FetchAST()
		if err != nil {
			log.Printf("[tools] AST fetch failed: %v", err)
		} else {
			helper.OOSAst = ast
			if role != "" {
				helper.ActiveRole = role
			}
			return formatAST(ast)
		}
	}

	if helper.OOSAst == nil {
		return "(schema not available — OOSP not connected)"
	}
	return formatAST(helper.OOSAst)
}

// formatAST marshals the AST to indented JSON.
func formatAST(ast *dsl.OOSAst) string {
	b, err := json.MarshalIndent(ast, "", "  ")
	if err != nil {
		return fmt.Sprintf("(AST marshal error: %v)", err)
	}
	return string(b)
}
