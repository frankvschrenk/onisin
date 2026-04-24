// Tool: go_hover / go_definition / go_references / go_diagnostics /
//       go_symbols / go_symbol_refs / go_package_diagnostics /
//       go_workspace_diagnostics
//
// These tools front the gopls Language Server. They complement the
// AST-based find_symbol/list_symbols (which run on a parsed file only)
// with full type-aware answers: hover gives the inferred type and
// godoc at any position, definition jumps across packages, references
// finds every usage workspace-wide, and diagnostics reports what the
// type-checker sees without a build round-trip.
//
// go_symbol_refs and go_package_diagnostics combine AST-level name
// resolution with gopls queries so common workflows ("who uses Foo?"
// and "is this package clean?") finish in one tool call instead of
// three. go_workspace_diagnostics extends package diagnostics to an
// entire subtree: "is this module clean?" in a single call.
//
// Positions are 1-based line/column as users naturally think of them
// and as editors display them. The LSP internally uses 0-based
// positions; conversion happens at the tool boundary.

package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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

	s.AddTool(mcp.NewTool("go_symbol_refs",
		mcp.WithDescription(
			"Find every usage of a Go symbol by name, across the workspace. "+
				"Combines AST-based symbol lookup with gopls references — no need "+
				"to first find the declaration's line/column by hand. Searches "+
				"inside 'path' (file or directory) for the declaration, then asks "+
				"gopls for all references. If the name occurs at more than one "+
				"declaration site (e.g. a method name shared across types), the "+
				"results include references for every candidate.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go symbol references")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File or directory to scan for the declaration")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Symbol name (e.g. 'Internal', 'NewManager', 'Client')")),
		mcp.WithBoolean("include_declaration", mcp.Description("Include the declaration itself in each result set (default: true)")),
	), ctx.handleGoSymbolRefs)

	s.AddTool(mcp.NewTool("go_package_diagnostics",
		mcp.WithDescription(
			"Run gopls diagnostics over every .go file in a package directory "+
				"and aggregate the results. The package is defined as all .go "+
				"files (including _test.go) in the given directory, non-recursive. "+
				"Use this as a quick 'is this package clean?' check after a "+
				"refactor — catches type errors, unused imports, and staticcheck "+
				"findings without a compile cycle.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go package diagnostics")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Package directory (non-recursive)")),
		mcp.WithBoolean("include_tests", mcp.Description("Include _test.go files (default: true)")),
		mcp.WithNumber("wait_ms", mcp.Description("Max time in milliseconds to wait for each file's diagnostics (default: 2000)")),
	), ctx.handleGoPackageDiagnostics)

	s.AddTool(mcp.NewTool("go_workspace_diagnostics",
		mcp.WithDescription(
			"Run gopls diagnostics across every Go package under a "+
				"directory tree. Recursively walks 'path', groups .go "+
				"files by their containing directory (one directory = "+
				"one package), and reports findings per package. Use as "+
				"a quick 'is this module clean?' check after a sweeping "+
				"refactor. Files are opened in batches to keep gopls "+
				"responsive on large trees.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Go workspace diagnostics")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory to scan recursively")),
		mcp.WithBoolean("include_tests", mcp.Description("Include _test.go files (default: true)")),
		mcp.WithNumber("wait_ms", mcp.Description("Max time in milliseconds to wait for each file's diagnostics (default: 2000)")),
		mcp.WithNumber("batch_size", mcp.Description("Number of files to open concurrently per batch (default: 20)")),
	), ctx.handleGoWorkspaceDiagnostics)
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

// handleGoSymbolRefs resolves a symbol name to one or more declaration
// sites under the given path, then runs go_references on each. The
// result is a map keyed by declaration location so the caller can tell
// apart references for two symbols that happen to share a name.
func (c *handlerCtx) handleGoSymbolRefs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	root, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_symbol_refs", err), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return c.errResult("go_symbol_refs", err), nil
	}
	includeDecl := optionalBool(req, "include_declaration", true)

	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, err := c.reg.Resolve(root)
	if err != nil {
		return c.errResult("go_symbol_refs", err), nil
	}

	decls, err := findSymbolDeclarations(abs, name)
	if err != nil {
		return c.errResult("go_symbol_refs", err), nil
	}
	if len(decls) == 0 {
		return jsonResult(map[string]any{
			"name":         name,
			"path":         abs,
			"count":        0,
			"declarations": []any{},
		}), nil
	}

	// Every declaration might live in a different workspace root.
	// Resolve each one's gopls client and issue references; skip
	// declarations the resolver can't attribute.
	declOut := make([]map[string]any, 0, len(decls))
	for _, d := range decls {
		goroot := c.goplsRootFor(d.File)
		if goroot == "" {
			declOut = append(declOut, map[string]any{
				"file":   d.File,
				"line":   d.Line,
				"column": d.Column,
				"error":  "no allowed root contains this file",
			})
			continue
		}
		cl, err := c.gopls.For(cctx, goroot)
		if err != nil {
			declOut = append(declOut, map[string]any{
				"file":   d.File,
				"line":   d.Line,
				"column": d.Column,
				"error":  err.Error(),
			})
			continue
		}
		locs, err := cl.References(cctx, d.File, d.Line-1, d.Column-1, includeDecl)
		if err != nil {
			declOut = append(declOut, map[string]any{
				"file":   d.File,
				"line":   d.Line,
				"column": d.Column,
				"error":  err.Error(),
			})
			continue
		}
		declOut = append(declOut, map[string]any{
			"file":      d.File,
			"line":      d.Line,
			"column":    d.Column,
			"kind":      d.Kind,
			"receiver":  d.Receiver,
			"signature": d.Signature,
			"count":     len(locs),
			"results":   locationsJSON(locs),
		})
	}

	return jsonResult(map[string]any{
		"name":         name,
		"path":         abs,
		"count":        len(declOut),
		"declarations": declOut,
	}), nil
}

// handleGoPackageDiagnostics iterates every .go file in the given
// directory (non-recursive) and aggregates gopls diagnostics. Files
// are opened in parallel via the same gopls client — the client's
// internal mutex serializes the underlying LSP writes. The per-file
// wait_ms bounds the total runtime.
func (c *handlerCtx) handleGoPackageDiagnostics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_package_diagnostics", err), nil
	}
	includeTests := optionalBool(req, "include_tests", true)
	waitMs := optionalInt(req, "wait_ms", 2000)

	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, err := c.reg.Resolve(dir)
	if err != nil {
		return c.errResult("go_package_diagnostics", err), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return c.errResult("go_package_diagnostics", err), nil
	}
	if !info.IsDir() {
		return c.errResult("go_package_diagnostics", fmt.Errorf("not a directory: %s", abs)), nil
	}

	files, err := goFilesIn(abs, includeTests)
	if err != nil {
		return c.errResult("go_package_diagnostics", err), nil
	}
	if len(files) == 0 {
		return jsonResult(map[string]any{
			"package": abs,
			"files":   0,
			"count":   0,
			"results": []any{},
		}), nil
	}

	root := c.goplsRootFor(abs)
	if root == "" {
		return c.errResult("go_package_diagnostics", fmt.Errorf("no allowed root contains %s", abs)), nil
	}
	cl, err := c.gopls.For(cctx, root)
	if err != nil {
		return c.errResult("go_package_diagnostics", err), nil
	}

	// Open every file first so gopls starts analyzing them in
	// parallel, then collect diagnostics with a bounded wait per file.
	for _, f := range files {
		if err := cl.EnsureOpen(cctx, f); err != nil {
			// An unreadable file shouldn't abort the whole package
			// scan — record it but continue.
			c.logger.Warn("go_package_diagnostics: open failed", "file", f, "err", err)
		}
	}

	all := make([]map[string]any, 0)
	fileSummary := make([]map[string]any, 0, len(files))
	for _, f := range files {
		waitCtx, cancelWait := context.WithTimeout(cctx, time.Duration(waitMs)*time.Millisecond)
		diags := cl.WaitForDiagnostics(waitCtx, f)
		cancelWait()
		fileSummary = append(fileSummary, map[string]any{
			"file":  f,
			"count": len(diags),
		})
		for _, d := range diags {
			all = append(all, map[string]any{
				"file":     f,
				"severity": gopls.SeverityName(d.Severity),
				"message":  d.Message,
				"source":   d.Source,
				"code":     d.Code,
				"range":    rangeJSON(d.Range),
			})
		}
	}

	return jsonResult(map[string]any{
		"package":   abs,
		"files":     len(files),
		"by_file":   fileSummary,
		"count":     len(all),
		"results":   all,
	}), nil
}

// handleGoWorkspaceDiagnostics runs gopls diagnostics across every Go
// package found under a directory tree. A package is every directory
// containing at least one .go file (non-test unless include_tests=true).
//
// All packages share a single gopls client — the one rooted at the
// workspace root covering the requested path. Files are opened in
// batches to keep gopls responsive: opening thousands of files at once
// stalls the type-checker; a small batch size (~20) lets it process
// packages incrementally while the tool collects diagnostics from the
// previous batch.
//
// The per-file wait_ms caps how long to wait for fresh diagnostics.
// Total runtime is bounded by callDeadline.
func (c *handlerCtx) handleGoWorkspaceDiagnostics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir, err := req.RequireString("path")
	if err != nil {
		return c.errResult("go_workspace_diagnostics", err), nil
	}
	includeTests := optionalBool(req, "include_tests", true)
	waitMs := optionalInt(req, "wait_ms", 2000)
	batchSize := optionalInt(req, "batch_size", 20)
	if batchSize < 1 {
		batchSize = 20
	}

	cctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()

	abs, err := c.reg.Resolve(dir)
	if err != nil {
		return c.errResult("go_workspace_diagnostics", err), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return c.errResult("go_workspace_diagnostics", err), nil
	}
	if !info.IsDir() {
		return c.errResult("go_workspace_diagnostics", fmt.Errorf("not a directory: %s", abs)), nil
	}

	packages, err := goPackageDirs(abs, includeTests)
	if err != nil {
		return c.errResult("go_workspace_diagnostics", err), nil
	}
	if len(packages) == 0 {
		return jsonResult(map[string]any{
			"root":       abs,
			"packages":   0,
			"files":     0,
			"count":     0,
			"by_package": []any{},
			"results":   []any{},
		}), nil
	}

	root := c.goplsRootFor(abs)
	if root == "" {
		return c.errResult("go_workspace_diagnostics", fmt.Errorf("no allowed root contains %s", abs)), nil
	}
	cl, err := c.gopls.For(cctx, root)
	if err != nil {
		return c.errResult("go_workspace_diagnostics", err), nil
	}

	// Flatten into a single file list so batching is uniform across
	// packages; track the originating package for the per-package
	// summary in the response.
	type fileEntry struct {
		pkg  string
		file string
	}
	var allFiles []fileEntry
	for pkg, files := range packages {
		for _, f := range files {
			allFiles = append(allFiles, fileEntry{pkg: pkg, file: f})
		}
	}

	perPackage := make(map[string]int, len(packages))
	all := make([]map[string]any, 0)
	totalFiles := 0

	// Open in batches, then collect diagnostics for each batch before
	// moving on. This spreads gopls's analysis work over time instead
	// of asking it to hold thousands of open files simultaneously.
	for start := 0; start < len(allFiles); start += batchSize {
		end := start + batchSize
		if end > len(allFiles) {
			end = len(allFiles)
		}
		batch := allFiles[start:end]

		for _, fe := range batch {
			if err := cl.EnsureOpen(cctx, fe.file); err != nil {
				c.logger.Warn("go_workspace_diagnostics: open failed",
					"file", fe.file, "err", err)
			}
		}
		for _, fe := range batch {
			waitCtx, cancelWait := context.WithTimeout(cctx, time.Duration(waitMs)*time.Millisecond)
			diags := cl.WaitForDiagnostics(waitCtx, fe.file)
			cancelWait()
			totalFiles++
			for _, d := range diags {
				all = append(all, map[string]any{
					"package":  fe.pkg,
					"file":     fe.file,
					"severity": gopls.SeverityName(d.Severity),
					"message":  d.Message,
					"source":   d.Source,
					"code":     d.Code,
					"range":    rangeJSON(d.Range),
				})
				perPackage[fe.pkg]++
			}
		}
	}

	// Build a stable by_package summary sorted by package path so the
	// output is deterministic across runs.
	pkgNames := make([]string, 0, len(packages))
	for pkg := range packages {
		pkgNames = append(pkgNames, pkg)
	}
	sort.Strings(pkgNames)
	byPackage := make([]map[string]any, 0, len(pkgNames))
	for _, pkg := range pkgNames {
		byPackage = append(byPackage, map[string]any{
			"package": pkg,
			"files":   len(packages[pkg]),
			"count":   perPackage[pkg],
		})
	}

	return jsonResult(map[string]any{
		"root":       abs,
		"packages":   len(packages),
		"files":      totalFiles,
		"count":      len(all),
		"by_package": byPackage,
		"results":    all,
	}), nil
}

// goPackageDirs walks root recursively and returns a map of
// package-directory → .go files in that directory. A "package" for the
// purposes of diagnostics is any directory with at least one .go file.
// Vendor, .git, and node_modules are skipped via heavyDirs.
func goPackageDirs(root string, includeTests bool) (map[string][]string, error) {
	out := make(map[string][]string)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			// Don't abort the whole walk for an unreadable subtree;
			// skip and continue.
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (heavyDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		dir := filepath.Dir(p)
		out[dir] = append(out[dir], p)
		return nil
	})
	return out, err
}

// symbolDecl is the position of one declaration found by AST scanning.
// Column is 1-based and points at the first character of the symbol
// name itself (not the keyword or receiver).
type symbolDecl struct {
	File      string
	Line      int
	Column    int
	Kind      string // "func" | "method" | "type" | "var" | "const"
	Receiver  string
	Signature string
}

// findSymbolDeclarations scans `root` (file or directory, recursively)
// for every top-level declaration whose name equals `name`. Unlike the
// existing find_symbol helper, it also records the precise column of
// the identifier so gopls can be queried at that exact position.
//
// Parse errors on individual files are swallowed — a broken file in an
// otherwise-healthy repo shouldn't abort the scan.
func findSymbolDeclarations(root, name string) ([]symbolDecl, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	var files []string
	if info.IsDir() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if heavyDirs[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".go") {
				files = append(files, p)
			}
			return nil
		})
	} else {
		if !strings.HasSuffix(root, ".go") {
			return nil, fmt.Errorf("not a Go file: %s", root)
		}
		files = []string{root}
	}

	fset := token.NewFileSet()
	var out []symbolDecl
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Name != name {
					continue
				}
				pos := fset.Position(d.Name.Pos())
				sd := symbolDecl{
					File:      f,
					Line:      pos.Line,
					Column:    pos.Column,
					Signature: renderFuncSignature(fset, d),
				}
				if d.Recv != nil && len(d.Recv.List) > 0 {
					sd.Kind = "method"
					sd.Receiver = receiverString(d.Recv.List[0])
				} else {
					sd.Kind = "func"
				}
				out = append(out, sd)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						if sp.Name.Name == name {
							pos := fset.Position(sp.Name.Pos())
							out = append(out, symbolDecl{
								File:   f,
								Line:   pos.Line,
								Column: pos.Column,
								Kind:   "type",
							})
						}
					case *ast.ValueSpec:
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						for _, ident := range sp.Names {
							if ident.Name == name {
								pos := fset.Position(ident.Pos())
								out = append(out, symbolDecl{
									File:   f,
									Line:   pos.Line,
									Column: pos.Column,
									Kind:   kind,
								})
							}
						}
					}
				}
			}
		}
	}
	return out, nil
}

// goFilesIn returns every .go file in dir (non-recursive). _test.go
// files are filtered out when includeTests is false.
func goFilesIn(dir string, includeTests bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	return out, nil
}
