package tools

// dsl_schema.go — DSL element retrieval for the chat agent.
//
// The agent loop in oos asks oosp for the small handful of DSL element
// chunks (one per <screen>, <section>, <field>, <tabs>, ...) that best
// match a natural-language layout intent. Each chunk combines XSD
// grammar facts (attributes, enums, valid children) with German
// enrichment (aliases, intent, copy-pasteable example, AI hints), so a
// single hit is enough material for the LLM to emit the corresponding
// DSL fragment.
//
// Retrieval is delegated entirely to oosp's POST /dsl/schema/search.
// oos never touches oos.oos_dsl_schema directly — it always goes
// through the REST seam, same as for the CTX retrieval path.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"onisin.com/oos/helper"
)

// DSLChunk is a single DSL element chunk returned by DSLSchemaSearch.
// The shape mirrors store.DSLChunk on the server side but is kept
// independent so the client compiles without importing oosp internals.
type DSLChunk struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Text string `json:"chunk"`
}

// DSLSchemaSearch queries oos.oos_dsl_schema via OOSP for the n DSL
// element chunks most relevant to query. The chunks are concatenated
// with a separator and returned ready for prompt injection.
//
// On parse failure we fall back to returning the raw response — the
// LLM can usually still extract useful structure from it, and a
// surfaced response is more debuggable than a swallowed error.
func DSLSchemaSearch(query string, n int) (string, error) {
	if helper.OOSP == nil {
		return "", fmt.Errorf("OOSP not connected")
	}
	if n <= 0 {
		n = 3
	}

	raw, err := helper.OOSP.Post("/dsl/schema/search", map[string]any{
		"query": query,
		"n":     n,
	})
	if err != nil {
		return "", fmt.Errorf("dsl schema search: %w", err)
	}

	var chunks []DSLChunk
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		log.Printf("[tools] dsl schema search parse error: %v — raw: %.200s", err, raw)
		return raw, nil
	}

	if len(chunks) == 0 {
		return "(no dsl element chunks found)", nil
	}

	var sb strings.Builder
	for i, c := range chunks {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(c.Text)
	}
	return sb.String(), nil
}
