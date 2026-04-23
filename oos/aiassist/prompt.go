package aiassist

// prompt.go — System prompt builder for the OOS AI assistant.
//
// The schema section is pre-built at session start (BuildSchemaPrompt)
// and cached in Session.schemaPrompt. This avoids an extra network call
// on every message and ensures the model always has schema context.
//
// Instruction section: comes from global.conf.xml via BuildGlobalPromptsSection.
// When authored, those prompts replace the hard-coded RULES/WORKFLOW so admins
// can change LLM behaviour without a client rebuild. When no global prompts
// are authored we fall back to the built-in defaults so the assistant always
// has some instructions to work with.

import (
	"strings"

	"onisin.com/oos/tools"
)

// fallbackInstructions are used when global.conf.xml has no <prompt> entries.
// They capture the minimum workflow the assistant needs to be useful:
// filter-argument shape, read-vs-write workflow, and the don't-explain rule.
// Once an admin authors global prompts these get superseded.
const fallbackInstructions = `## Instructions

1. Use the OOS SCHEMA above to find context names and GraphQL structure.
2. For compact/rag strategy: call oos_schema_search to get exact field names and filter syntax.
3. Filters use ONLY suffix arguments: age_gt, age_lt, city_like — NEVER use "where" or nested objects like {gt: x}.
4. When the user wants to see data → call oos_query with context and GraphQL query.
5. For lists: context ends with "_list". For details: ends with "_detail".
6. NEVER write or delete data without explicit user confirmation.
7. Do not explain what you are about to do — just do it.

## Workflow

  User asks for data:
    1. (compact/rag) oos_schema_search  → get exact GraphQL fields and filter examples
    2. oos_query                        → load and render data in the board

  User wants to change data:
    1. oos_schema_search  → find context
    2. oos_query          → load current record
    3. oos_ui_change_required → preview changes
    4. oos_ui_save        → save after explicit user confirmation`

// buildSystemPrompt assembles the full system prompt for a session.
// schemaBlock is the pre-loaded schema section (compact / full / rag hint).
//
// Assembly order:
//   1. User context (who is talking, which role) — short, one line.
//   2. Identity sentence ("You are OOS Assistant ...").
//   3. Schema block — authoritative source of truth for fields and queries.
//   4. Instructions — either from global.conf.xml or the fallback above.
//
// Schema comes before instructions because instructions often reference
// schema vocabulary ("use list_fields of the context") and the model reads
// top-down.
func buildSystemPrompt(schemaBlock string) string {
	var sb strings.Builder

	userCtx := tools.SystemContext()
	if userCtx != "" {
		sb.WriteString(userCtx)
		sb.WriteString("\n")
	}

	sb.WriteString("You are OOS Assistant — a precise data assistant.\n\n")

	sb.WriteString(schemaBlock)
	sb.WriteString("\n")

	if globalPrompts := tools.BuildGlobalPromptsSection(); globalPrompts != "" {
		sb.WriteString(globalPrompts)
	} else {
		sb.WriteString(fallbackInstructions)
	}

	return sb.String()
}
