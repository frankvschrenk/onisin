package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"onisin.com/oosfs/internal/roots"
)

// TestPGLiveAgainstDemoDB exercises the real handlers against the demo
// database configured in demo.toml. It is skipped when demo.toml cannot
// be found (e.g. in CI without a running Postgres), so the unit test
// suite stays hermetic while local developers can still get quick
// feedback that their pg_* tools talk to the right database.
//
// We keep this intentionally read-only: the test runs pg_query, not
// pg_exec or pg_reset. Verifying reset/exec in an automated test would
// require a disposable database — out of scope for a quick smoke test.
func TestPGLiveAgainstDemoDB(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Build a Registry over the onisin repo root; demo.toml sits there.
	reg, err := roots.New([]string{"/Users/frank/repro/onisin"}, logger)
	if err != nil {
		t.Fatalf("roots: %v", err)
	}

	cfg := findDemoTOML(reg, logger)
	if cfg == nil {
		t.Skip("no demo.toml found — not a problem, just nothing to exercise here")
	}

	pgCtx := &pgHandlerCtx{
		handlerCtx: &handlerCtx{reg: reg, logger: logger},
		cfg:        cfg,
	}

	// A trivial query that works on any reachable Postgres: ask it what
	// version it is. If this succeeds we know the DSN resolves, the
	// superuser credentials are correct, and the row → JSON pipeline
	// does something sensible.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"sql": "SELECT version() AS server_version",
	}
	result, err := pgCtx.handleQuery(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQuery transport err: %v", err)
	}
	if result.IsError {
		t.Fatalf("handleQuery tool err: %+v", result.Content)
	}

	// Unwrap the JSON text payload and confirm row 0 has a plausible
	// Postgres version string.
	var payload struct {
		RowCount int                      `json:"row_count"`
		Rows     []map[string]interface{} `json:"rows"`
	}
	text := resultText(t, result)
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("payload unmarshal: %v\npayload: %s", err, text)
	}
	if payload.RowCount != 1 {
		t.Fatalf("row_count: got %d, want 1", payload.RowCount)
	}
	ver, _ := payload.Rows[0]["server_version"].(string)
	if len(ver) < 5 || ver[:10] != "PostgreSQL" {
		t.Fatalf("server_version looks wrong: %q", ver)
	}
	t.Logf("connected: %s", ver)
}

// resultText extracts the first text block from a CallToolResult.
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	for _, block := range r.Content {
		if tc, ok := block.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatalf("no text content in result")
	return ""
}
