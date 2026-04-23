// Tool: find_symbol / list_symbols
//
// Go-AST-powered symbol navigation. These tools bypass text search
// completely and use go/parser so the results are semantically exact:
// "where is type EventProcessor defined?" returns just the definition,
// not every callsite.
//
// find_symbol locates a symbol by name across a tree of Go files.
// list_symbols dumps every top-level declaration in a given package or
// file. Both produce structured JSON with file, line, kind, receiver,
// and exported/unexported status.

package tools

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// symbol is one top-level Go declaration worth reporting.
//
// Signature is only populated for funcs and methods; it holds the
// canonical declaration line (body stripped) so that callers can wire
// up a call site without re-reading the source file.
//
// TypeKind is only populated for Kind=="type" and reports the underlying
// category — "struct", "interface", "alias", "func", "map", "chan",
// "slice", "array", or "ident" — again to save a round-trip for the
// common question "what kind of thing is this type?".
type symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "func" | "method" | "type" | "var" | "const"
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line,omitempty"`
	Receiver  string `json:"receiver,omitempty"`  // for methods: "(p *EventProcessor)"
	Signature string `json:"signature,omitempty"` // for funcs/methods: full declaration line
	TypeKind  string `json:"type_kind,omitempty"` // for types: "struct", "interface", "alias", ...
	Exported  bool   `json:"exported"`
	Doc       string `json:"doc,omitempty"` // leading godoc comment, first line only
	Package   string `json:"package,omitempty"`
}

func registerSymbols(s *server.MCPServer, ctx *handlerCtx) {
	findTool := mcp.NewTool("find_symbol",
		mcp.WithDescription(
			"Find Go symbol definitions by name across a directory tree. "+
				"Uses go/parser so results are semantically exact, not text "+
				"matches. Returns all declarations whose name equals or matches "+
				"the regex, across funcs, methods, types, vars, and constants.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Find Go symbol")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory to scan")),
		mcp.WithString("name", mcp.Description("Exact symbol name (mutually exclusive with 'pattern')")),
		mcp.WithString("pattern", mcp.Description("Regex over symbol names (Go RE2); overrides 'name' if both are given")),
		mcp.WithArray("kinds",
			mcp.Description("Filter by kinds: 'func', 'method', 'type', 'var', 'const'. Default: all."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("exported_only", mcp.Description("Only return exported (capitalized) symbols (default: false)")),
		mcp.WithBoolean("include_heavy", mcp.Description("Descend into vendor/.git/node_modules (default: false)")),
	)
	s.AddTool(findTool, ctx.handleFindSymbol)

	listTool := mcp.NewTool("list_symbols",
		mcp.WithDescription(
			"List every top-level symbol (func, method, type, var, const) in "+
				"a single Go file or one package directory. Handy for getting "+
				"a quick overview of a package's surface area without reading "+
				"every file.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("List Go symbols")),
		mcp.WithString("path", mcp.Required(), mcp.Description("A .go file or a directory containing .go files")),
		mcp.WithBoolean("exported_only", mcp.Description("Only return exported symbols (default: false)")),
	)
	s.AddTool(listTool, ctx.handleListSymbols)
}

func (c *handlerCtx) handleFindSymbol(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	root, err := req.RequireString("path")
	if err != nil {
		return c.errResult("find_symbol", err), nil
	}
	name := optionalString(req, "name", "")
	pattern := optionalString(req, "pattern", "")
	exportedOnly := optionalBool(req, "exported_only", false)
	includeHeavy := optionalBool(req, "include_heavy", false)
	kinds := toStringSet(optionalStringSlice(req, "kinds"))

	if name == "" && pattern == "" {
		return c.errResult("find_symbol", fmt.Errorf("one of 'name' or 'pattern' is required")), nil
	}

	abs, err := c.reg.Resolve(root)
	if err != nil {
		return c.errResult("find_symbol", err), nil
	}

	matcher, err := buildSymbolMatcher(name, pattern)
	if err != nil {
		return c.errResult("find_symbol", err), nil
	}

	var results []symbol
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if !includeHeavy && heavyDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			// _test.go files are handled only in list_symbols for now;
			// finding symbols in tests rarely matters for navigation.
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
		}
		syms, err := parseFileSymbols(p)
		if err != nil {
			// Parse errors on one file shouldn't abort the whole scan.
			return nil
		}
		for _, s := range syms {
			if !matcher(s.Name) {
				continue
			}
			if exportedOnly && !s.Exported {
				continue
			}
			if len(kinds) > 0 && !kinds[s.Kind] {
				continue
			}
			results = append(results, s)
		}
		return nil
	})
	if walkErr != nil {
		return c.errResult("find_symbol", walkErr), nil
	}

	return jsonResult(map[string]any{
		"root":    abs,
		"name":    name,
		"pattern": pattern,
		"count":   len(results),
		"results": results,
	}), nil
}

func (c *handlerCtx) handleListSymbols(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := req.RequireString("path")
	if err != nil {
		return c.errResult("list_symbols", err), nil
	}
	exportedOnly := optionalBool(req, "exported_only", false)

	abs, err := c.reg.Resolve(p)
	if err != nil {
		return c.errResult("list_symbols", err), nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		return c.errResult("list_symbols", err), nil
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return c.errResult("list_symbols", err), nil
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				files = append(files, filepath.Join(abs, e.Name()))
			}
		}
	} else {
		if !strings.HasSuffix(abs, ".go") {
			return c.errResult("list_symbols", fmt.Errorf("not a Go file: %s", abs)), nil
		}
		files = []string{abs}
	}

	var results []symbol
	for _, f := range files {
		syms, err := parseFileSymbols(f)
		if err != nil {
			continue
		}
		for _, s := range syms {
			if exportedOnly && !s.Exported {
				continue
			}
			results = append(results, s)
		}
	}

	return jsonResult(map[string]any{
		"path":    abs,
		"count":   len(results),
		"results": results,
	}), nil
}

// parseFileSymbols parses a single Go source file and returns all
// top-level declarations as symbols.
//
// Uses parser.ParseComments so leading godoc comments are available for
// the Doc field. We intentionally do not run go/types — that would force
// full package resolution and vastly complicate the code. Surface-level
// navigation works fine without it.
func parseFileSymbols(path string) ([]symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := file.Name.Name
	var out []symbol

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			s := symbol{
				Name:      d.Name.Name,
				Kind:      "func",
				File:      path,
				Line:      fset.Position(d.Pos()).Line,
				EndLine:   fset.Position(d.End()).Line,
				Exported:  ast.IsExported(d.Name.Name),
				Doc:       firstDocLine(d.Doc),
				Package:   pkg,
				Signature: renderFuncSignature(fset, d),
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind = "method"
				s.Receiver = receiverString(d.Recv.List[0])
			}
			out = append(out, s)

		case *ast.GenDecl:
			// GenDecl groups var, const, type, import. We unpack each spec
			// so that "var a, b int" reports as two symbols.
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, symbol{
						Name:     sp.Name.Name,
						Kind:     "type",
						File:     path,
						Line:     fset.Position(sp.Pos()).Line,
						EndLine:  fset.Position(sp.End()).Line,
						Exported: ast.IsExported(sp.Name.Name),
						Doc:      firstDocLine(d.Doc),
						Package:  pkg,
						TypeKind: typeKind(sp),
					})
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, n := range sp.Names {
						out = append(out, symbol{
							Name:     n.Name,
							Kind:     kind,
							File:     path,
							Line:     fset.Position(n.Pos()).Line,
							EndLine:  fset.Position(n.End()).Line,
							Exported: ast.IsExported(n.Name),
							Doc:      firstDocLine(d.Doc),
							Package:  pkg,
						})
					}
				}
			}
		}
	}
	return out, nil
}

// renderFuncSignature returns the canonical declaration line for a
// function or method — e.g. "func (p *PGImporter) LoadCTXRaw(ctx
// context.Context, id string) (string, error)" — by printing the AST
// node with Body and Doc stripped.
//
// Stripping the body is the whole trick: go/printer happily prints a
// FuncDecl without a block, which gives us exactly the one line we want.
// Doc is cleared too so the printer doesn't re-emit the godoc comment
// (the symbol struct already carries that separately).
//
// The node is shallow-copied before mutation so the caller's AST stays
// pristine — important because the same file is sometimes parsed once
// and inspected by several code paths.
func renderFuncSignature(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn == nil || fn.Name == nil {
		return ""
	}
	stripped := *fn
	stripped.Body = nil
	stripped.Doc = nil

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces, Tabwidth: 4}
	if err := cfg.Fprint(&buf, fset, &stripped); err != nil {
		return ""
	}
	// A few printers emit a trailing newline; collapse internal
	// whitespace so the result fits on one line even for multi-line
	// parameter lists in the source.
	return collapseWhitespace(buf.String())
}

// typeKind categorises a TypeSpec by its underlying AST node. Covers the
// common cases a developer asks about when browsing a package — struct,
// interface, type alias — and falls back to "ident" / "expr" for the
// long tail.
func typeKind(sp *ast.TypeSpec) string {
	if sp == nil || sp.Type == nil {
		return ""
	}
	if sp.Assign.IsValid() {
		// "type Foo = Bar" is an alias, regardless of what Bar is.
		return "alias"
	}
	switch sp.Type.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.FuncType:
		return "func"
	case *ast.MapType:
		return "map"
	case *ast.ChanType:
		return "chan"
	case *ast.ArrayType:
		// Go's AST uses ArrayType for both arrays and slices; the
		// difference is whether Len is nil.
		if at, ok := sp.Type.(*ast.ArrayType); ok && at.Len == nil {
			return "slice"
		}
		return "array"
	case *ast.Ident, *ast.SelectorExpr, *ast.StarExpr:
		return "ident"
	}
	return "expr"
}

// collapseWhitespace folds newlines and runs of spaces/tabs into single
// spaces. Used to render multi-line function signatures as a single line
// without disturbing the relative order of tokens.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\n' || r == '\t' || r == ' ' {
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	return strings.TrimSpace(b.String())
}

// receiverString builds a compact receiver description for methods,
// e.g. "(p *EventProcessor)" or "(s Store)".
func receiverString(field *ast.Field) string {
	if field == nil {
		return ""
	}
	var name string
	if len(field.Names) > 0 {
		name = field.Names[0].Name + " "
	}
	switch t := field.Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(" + name + "*" + id.Name + ")"
		}
	case *ast.Ident:
		return "(" + name + t.Name + ")"
	}
	return ""
}

// firstDocLine returns the first line of a doc comment block, trimmed of
// comment markers. Full doc dumps would bloat the response; one line is
// enough for an LLM to decide if a symbol is interesting.
func firstDocLine(g *ast.CommentGroup) string {
	if g == nil || len(g.List) == 0 {
		return ""
	}
	line := g.List[0].Text
	line = strings.TrimPrefix(line, "//")
	line = strings.TrimPrefix(line, "/*")
	line = strings.TrimSuffix(line, "*/")
	line = strings.TrimSpace(line)
	// The first "line" of a godoc often starts with the symbol name
	// itself ("Foo does X"). Keep it as-is — the LLM knows the convention.
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	return line
}

// buildSymbolMatcher returns a predicate over symbol names based on the
// caller's 'name' or 'pattern' argument.
func buildSymbolMatcher(name, pattern string) (func(string) bool, error) {
	if pattern != "" {
		re, err := compileRegex(pattern, false)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	return func(s string) bool { return s == name }, nil
}

// toStringSet turns a slice into a lookup set. Cheaper than repeatedly
// scanning a slice and makes the caller code read like filter logic.
func toStringSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}
