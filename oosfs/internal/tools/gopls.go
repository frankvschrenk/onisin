// Tool: go_hover / go_definition / go_references / go_diagnostics / go_symbols
//
// These tools front the gopls Language Server. They complement the
// AST-based find_symbol/list_symbols (which run on a parsed file only)
// with full type-aware answers: hover gives the inferred type and
// godoc at any position, definition jumps across packages, references
// finds every usage workspace-wide, and diagnostics reports what the
// type-checker sees without a build round-trip.
//
// Positions are 1-based line/column as users naturally think of them
// and as editors display them. The LSP internally uses 0-based
// positions; conversion happens at the tool boundary.

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"onisin.com/oosfs/internal/gopls"
)

func registerGopls(s *server.MCPServer, ctx *handlerCtx) {
	s.AddTool(mcp.NewTool("go_hover",
		mcp.WithDescription(
			"Ask gopls what a position in a Go file is. Returns the inferred type, "+
				"the godoc comment, and (for calls) the target signature. Position is "+
				"1-based line/column as shown in editors.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go hover")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or root-relative path to a .go file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-based line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-based column (character offset)")),
	), ctx.handleGoHover)

	s.AddTool(mcp.NewTool("go_definition",
		mcp.WithDescription(
			"Ask gopls where the symbol under the cursor is defined. Unlike "+
				"find_symbol (which matches by name) this disambiguates shadowed or "+
				"package-local names and works across module boundaries.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go definition")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or root-relative path to a .go file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-based line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-based column (character offset)")),
	), ctx.handleGoDefinition)

	s.AddTool(mcp.NewTool("go_references",
		mcp.WithDescription(
			"List every usage of the symbol under the cursor across the workspace. "+
				"Semantically exact — beats text search for any identifier that is "+
				"also a common English word.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go references")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or root-relative path to a .go file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-based line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-based column (character offset)")),
		mcp.WithBoolean("include_declaration", mcp.Description("Include the declaration itself in the results (default: true)")),
	), ctx.handleGoReferences)

	s.AddTool(mcp.NewTool("go_diagnostics",
		mcp.WithDescription(
			"Report gopls's current diagnostics (errors, warnings, hints) for a "+
				"Go file. Picks up type errors, unused imports, and staticcheck "+
				"findings without running the compiler.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go diagnostics")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or root-relative path to a .go file")),
		mcp.WithNumber("wait_ms", mcp.Description("Max time in milliseconds to wait for fresh diagnostics (default: 2000)")),
	), ctx.handleGoDiagnostics)

	s.AddTool(mcp.NewTool("go_symbols",
		mcp.WithDescription(
			"Return the gopls document-symbol tree for a Go file — functions, "+
				"types, fields, methods — with their ranges. Complementary to "+
				"list_symbols: gopls sees what the type-checker sees, including "+
				"struct fields and interface methods with their detail lines.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go symbols")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or root-relative path to a .go file")),
	), ctx.handleGoSymbols)
}

// callDeadline is the default per-request timeout. gopls can be slow on
// first contact with a large module (initial workspace load), so we give
// generous headroom; hover on a warm cache takes a few milliseconds.
const callDeadline = 45 * time.Second

// resolveGoFile resolves the caller-supplied path and confirms it is a
// .go file. Returns the absolute path and the gopls client for its
// workspace root.
func (c *handlerCtx) resolveGoFile(ctx context.Context, op, path string) (string, *gopls.Client, error) {
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	if !strings.HasSuffix(abs, ".go") {
		return "", nil, fmt.Errorf("not a Go file: %s", abs)
	}
	root := c.goplsRootFor(abs)
	if root == "" {
		return "", nil, fmt.Errorf("no allowed root contains %s", abs)
	}
	cl, err := c.gopls.For(ctx, root)
	if err != nil {
		return "", nil, err
	}
	return abs, cl, nil
}

// goplsRootFor picks the allowed root that contains absPath. If more
// than one allowed root matches (nested roots), the longest — i.e. most
// specific — prefix wins, giving the smallest workspace gopls has to
// manage.
func (c *handlerCtx) goplsRootFor(absPath string) string {
	best := ""
	for _, r := range c.reg.All() {
		if strings.HasPrefix(absPath, r+"/") || absPath == r {
			if len(r) > len(best) {
				best = r
			}
		}
	}
	return best
}

func (c *handlerCtx) handleGoHover(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_hover", err), nil
	}
	line, col, err := positionArgs(req)
	if err != nil {
		return c.errResult("go_hover", err), nil
	}
	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, cl, err := c.resolveGoFile(cctx, "go_hover", path)
	if err != nil {
		return c.errResult("go_hover", err), nil
	}
	hover, err := cl.Hover(cctx, abs, line-1, col-1)
	if err != nil {
		return c.errResult("go_hover", err), nil
	}
	if hover == nil {
		return jsonResult(map[string]any{
			"file":  abs,
			"line":  line,
			"found": false,
		}), nil
	}
	out := map[string]any{
		"file":  abs,
		"line":  line,
		"found": true,
		"kind":  hover.Contents.Kind,
		"value": hover.Contents.Value,
	}
	if hover.Range != nil {
		out["range"] = rangeJSON(*hover.Range)
	}
	return jsonResult(out), nil
}

func (c *handlerCtx) handleGoDefinition(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_definition", err), nil
	}
	line, col, err := positionArgs(req)
	if err != nil {
		return c.errResult("go_definition", err), nil
	}
	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, cl, err := c.resolveGoFile(cctx, "go_definition", path)
	if err != nil {
		return c.errResult("go_definition", err), nil
	}
	locs, err := cl.Definition(cctx, abs, line-1, col-1)
	if err != nil {
		return c.errResult("go_definition", err), nil
	}
	return jsonResult(map[string]any{
		"file":    abs,
		"line":    line,
		"count":   len(locs),
		"results": locationsJSON(locs),
	}), nil
}

func (c *handlerCtx) handleGoReferences(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_references", err), nil
	}
	line, col, err := positionArgs(req)
	if err != nil {
		return c.errResult("go_references", err), nil
	}
	includeDecl := optionalBool(req, "include_declaration", true)
	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, cl, err := c.resolveGoFile(cctx, "go_references", path)
	if err != nil {
		return c.errResult("go_references", err), nil
	}
	locs, err := cl.References(cctx, abs, line-1, col-1, includeDecl)
	if err != nil {
		return c.errResult("go_references", err), nil
	}
	return jsonResult(map[string]any{
		"file":    abs,
		"line":    line,
		"count":   len(locs),
		"results": locationsJSON(locs),
	}), nil
}

func (c *handlerCtx) handleGoDiagnostics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_diagnostics", err), nil
	}
	waitMs := optionalInt(req, "wait_ms", 2000)
	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, cl, err := c.resolveGoFile(cctx, "go_diagnostics", path)
	if err != nil {
		return c.errResult("go_diagnostics", err), nil
	}
	// Opening the file triggers a fresh analysis on gopls's side.
	if err := cl.EnsureOpen(cctx, abs); err != nil {
		return c.errResult("go_diagnostics", err), nil
	}
	waitCtx, cancelWait := context.WithTimeout(cctx, time.Duration(waitMs)*time.Millisecond)
	diags := cl.WaitForDiagnostics(waitCtx, abs)
	cancelWait()

	out := make([]map[string]any, 0, len(diags))
	for _, d := range diags {
		out = append(out, map[string]any{
			"severity": gopls.SeverityName(d.Severity),
			"message":  d.Message,
			"source":   d.Source,
			"code":     d.Code,
			"range":    rangeJSON(d.Range),
		})
	}
	return jsonResult(map[string]any{
		"file":    abs,
		"count":   len(out),
		"results": out,
	}), nil
}

func (c *handlerCtx) handleGoSymbols(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_symbols", err), nil
	}
	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, cl, err := c.resolveGoFile(cctx, "go_symbols", path)
	if err != nil {
		return c.errResult("go_symbols", err), nil
	}
	syms, err := cl.DocumentSymbol(cctx, abs)
	if err != nil {
		return c.errResult("go_symbols", err), nil
	}
	return jsonResult(map[string]any{
		"file":    abs,
		"count":   len(syms),
		"results": documentSymbolsJSON(syms),
	}), nil
}

// positionArgs extracts the required line/column arguments and converts
// them to 0-based LSP positions.
func positionArgs(req mcp.CallToolRequest) (int, int, error) {
	l := optionalInt(req, "line", 0)
	c := optionalInt(req, "column", 0)
	if l <= 0 || c <= 0 {
		return 0, 0, fmt.Errorf("line and column are required and must be 1-based positive integers")
	}
	return l, c, nil
}

// rangeJSON flattens an LSP Range into 1-based coordinates for output.
func rangeJSON(r gopls.Range) map[string]any {
	return map[string]any{
		"start_line": r.Start.Line + 1,
		"start_col":  r.Start.Character + 1,
		"end_line":   r.End.Line + 1,
		"end_col":    r.End.Character + 1,
	}
}

// locationsJSON normalizes LSP Location results for tool output: the
// URI is translated back to a filesystem path, and the range is turned
// into 1-based coordinates.
func locationsJSON(locs []gopls.Location) []map[string]any {
	out := make([]map[string]any, 0, len(locs))
	for _, l := range locs {
		out = append(out, map[string]any{
			"file":  uriToFilesystemPath(l.URI),
			"range": rangeJSON(l.Range),
		})
	}
	return out
}

// documentSymbolsJSON converts gopls DocumentSymbol trees into a shape
// friendlier to LLM consumers — flat field names, 1-based ranges, and
// human-readable kind strings. Children are preserved hierarchically.
func documentSymbolsJSON(syms []gopls.DocumentSymbol) []map[string]any {
	out := make([]map[string]any, 0, len(syms))
	for _, s := range syms {
		entry := map[string]any{
			"name":            s.Name,
			"kind":            gopls.SymbolKindName[s.Kind],
			"detail":          s.Detail,
			"range":           rangeJSON(s.Range),
			"selection_range": rangeJSON(s.SelectionRange),
		}
		if len(s.Children) > 0 {
			entry["children"] = documentSymbolsJSON(s.Children)
		}
		out = append(out, entry)
	}
	return out
}

// uriToFilesystemPath is a tool-layer helper that mirrors gopls.uriToPath
// but lives here because the gopls package keeps its helper unexported.
// Invalid URIs are returned unchanged so the caller can still see what
// gopls sent.
func uriToFilesystemPath(uri string) string {
	const prefix = "file://"
	if !strings.HasPrefix(uri, prefix) {
		return uri
	}
	return uri[len(prefix):]
}
