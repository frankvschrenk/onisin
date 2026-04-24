// Package tools implements the MCP tool handlers exposed by oosfs.
//
// Each tool lives in its own file and registers itself via a Register
// function called from RegisterAll. Tool outputs are always JSON with a
// stable shape — this makes results easy for an LLM to consume and easy
// for a human to diff.
package tools

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"onisin.com/oosfs/internal/gopls"
	"onisin.com/oosfs/internal/roots"
)

// trustedMode reports whether oosfs was started in trusted mode. In
// trusted mode every tool is advertised as read-only to the client, which
// suppresses the per-call confirmation dialog in Claude Desktop. The
// actual behaviour of the tools is unchanged — the annotation is a pure
// UX hint. Enable via OOSFS_TRUSTED=1 at startup.
func trustedMode() bool {
	v := os.Getenv("OOSFS_TRUSTED")
	return v == "1" || v == "true" || v == "yes"
}

// RegisterAll wires every tool into the given MCP server.
//
// New tools are added here in one place. Keeping the list flat makes it
// obvious what oosfs can do and simplifies code review.
func RegisterAll(s *server.MCPServer, reg *roots.Registry, logger *slog.Logger) {
	ctx := &handlerCtx{
		reg:    reg,
		logger: logger,
		gopls:  gopls.NewManager(logger),
	}

	registerList(s, ctx)
	registerRead(s, ctx)
	registerSearch(s, ctx)
	registerWrite(s, ctx)
	registerMeta(s, ctx)
	registerExec(s, ctx)
	registerExecStream(s, ctx)
	registerPatch(s, ctx)
	registerSymbols(s, ctx)
	registerGopls(s, ctx)
	registerPostgres(s, ctx)
	registerMemory(s, ctx)
	registerBrowser(s, ctx)
}

// handlerCtx bundles the shared state every tool handler needs.
//
// gopls is a *gopls.Manager that owns one long-lived gopls subprocess
// per workspace root. It is lazily populated — the first Go LSP request
// for a root spawns gopls for that root. Manager is always non-nil so
// handlers can call it without a guard.
type handlerCtx struct {
	reg    *roots.Registry
	logger *slog.Logger
	gopls  *gopls.Manager
}

// jsonResult marshals v and wraps it in an MCP text result. Callers can
// ignore the error: json.Marshal only fails on unsupported types, and all
// payloads defined in oosfs are plain structs and maps.
func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError("marshal result: " + err.Error())
	}
	return mcp.NewToolResultText(string(data))
}

// errResult logs the error and returns an MCP error result. Returning the
// error via the result (not via the second return value of the handler)
// lets the LLM see the message instead of receiving a protocol-level fault.
func (c *handlerCtx) errResult(op string, err error) *mcp.CallToolResult {
	c.logger.Warn("tool error", "op", op, "err", err)
	return mcp.NewToolResultError(err.Error())
}

// readOnlyAnnotations returns the annotations appropriate for tools that
// only observe the filesystem. UIs like Claude Desktop use these hints to
// suppress confirmation dialogs on safe operations.
//
// IdempotentHint is set to true because re-reading the same file yields
// the same result (modulo concurrent writers, which is not an issue here).
// OpenWorldHint is false — the tool touches local state only.
func readOnlyAnnotations(title string) mcp.ToolAnnotation {
	t := true
	f := false
	return mcp.ToolAnnotation{
		Title:           title,
		ReadOnlyHint:    &t,
		DestructiveHint: &f,
		IdempotentHint:  &t,
		OpenWorldHint:   &f,
	}
}

// writeAnnotations returns annotations for tools that modify files but can
// safely be retried (same input → same post-condition), such as `write`
// and `mkdir -p`.
//
// In trusted mode the tool is advertised as read-only so the client skips
// the confirmation dialog. Actual behaviour is unchanged.
func writeAnnotations(title string) mcp.ToolAnnotation {
	if trustedMode() {
		return readOnlyAnnotations(title)
	}
	t := true
	f := false
	return mcp.ToolAnnotation{
		Title:           title,
		ReadOnlyHint:    &f,
		DestructiveHint: &f,
		IdempotentHint:  &t,
		OpenWorldHint:   &f,
	}
}

// destructiveAnnotations returns annotations for tools whose effect cannot
// easily be undone: `remove`, `move`, `edit` (overwrite), and `append`
// (not idempotent — each call grows the file).
//
// In trusted mode these are also advertised as read-only; see the note on
// writeAnnotations.
func destructiveAnnotations(title string, idempotent bool) mcp.ToolAnnotation {
	if trustedMode() {
		return readOnlyAnnotations(title)
	}
	t := true
	f := false
	ann := mcp.ToolAnnotation{
		Title:           title,
		ReadOnlyHint:    &f,
		DestructiveHint: &t,
		OpenWorldHint:   &f,
	}
	if idempotent {
		ann.IdempotentHint = &t
	} else {
		ann.IdempotentHint = &f
	}
	return ann
}
