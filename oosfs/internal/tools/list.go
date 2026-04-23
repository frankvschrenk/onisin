// Tool: list / tree
//
// list returns a flat directory listing as structured JSON.
// tree returns a recursive view with configurable depth.
//
// Both tools emit JSON rather than "[FILE] foo.txt" lines so the caller can
// sort, filter, or count without parsing a bespoke text format.

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// entry describes a single filesystem entry in tool output.
type entry struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"` // "file" | "dir" | "symlink" | "other"
	Size      int64   `json:"size,omitempty"`
	ModTime   string  `json:"mtime"`
	Mode      string  `json:"mode"`
	IsSymlink bool    `json:"is_symlink,omitempty"`
	Target    string  `json:"target,omitempty"` // symlink target, if any
	Children  []entry `json:"children,omitempty"`
}

func registerList(s *server.MCPServer, ctx *handlerCtx) {
	listTool := mcp.NewTool("list",
		mcp.WithDescription("List directory entries as structured JSON. Each entry contains name, type, size, mtime, mode and symlink info. Results are sorted with directories first, then alphabetically."),
		mcp.WithToolAnnotation(readOnlyAnnotations("List directory")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory to list")),
		mcp.WithBoolean("hidden", mcp.Description("Include dotfiles (default: false)")),
	)
	s.AddTool(listTool, ctx.handleList)

	treeTool := mcp.NewTool("tree",
		mcp.WithDescription("Recursive directory tree as structured JSON. Respects a depth limit and always skips .git, node_modules, and dist unless explicitly requested."),
		mcp.WithToolAnnotation(readOnlyAnnotations("Directory tree")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Root directory for the tree")),
		mcp.WithNumber("depth", mcp.Description("Maximum recursion depth (default: 3, use 0 for unlimited)")),
		mcp.WithBoolean("hidden", mcp.Description("Include dotfiles (default: false)")),
		mcp.WithBoolean("include_heavy", mcp.Description("Descend into .git / node_modules / dist (default: false)")),
	)
	s.AddTool(treeTool, ctx.handleTree)
}

func (c *handlerCtx) handleList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("list", err), nil
	}
	hidden := optionalBool(req, "hidden", false)

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("list", err), nil
	}

	entries, err := readDir(abs, hidden)
	if err != nil {
		return c.errResult("list", err), nil
	}
	return jsonResult(map[string]any{
		"path":    abs,
		"count":   len(entries),
		"entries": entries,
	}), nil
}

func (c *handlerCtx) handleTree(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("tree", err), nil
	}
	depth := optionalInt(req, "depth", 3)
	hidden := optionalBool(req, "hidden", false)
	includeHeavy := optionalBool(req, "include_heavy", false)

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("tree", err), nil
	}

	root, truncated, err := buildTree(abs, 0, depth, hidden, includeHeavy)
	if err != nil {
		return c.errResult("tree", err), nil
	}
	return jsonResult(map[string]any{
		"path":      abs,
		"truncated": truncated, // true if depth limit was hit somewhere
		"root":      root,
	}), nil
}

// readDir returns a sorted slice of entries for a single directory.
// Directories come first, then files, each group alphabetically.
func readDir(dir string, includeHidden bool) ([]entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(items))
	for _, item := range items {
		if !includeHidden && strings.HasPrefix(item.Name(), ".") {
			continue
		}
		e, err := statEntry(filepath.Join(dir, item.Name()))
		if err != nil {
			// Unreadable entries are reported with an error name but we do
			// not abort — one broken symlink shouldn't kill a listing.
			out = append(out, entry{Name: item.Name(), Type: "error", Mode: err.Error()})
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Type == "dir") != (out[j].Type == "dir") {
			return out[i].Type == "dir"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// statEntry builds an entry for a single path, following symlinks lazily so
// broken links are still reported rather than erroring.
func statEntry(path string) (entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return entry{}, err
	}
	e := entry{
		Name:    info.Name(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		Mode:    info.Mode().String(),
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		e.Type = "symlink"
		e.IsSymlink = true
		if target, err := os.Readlink(path); err == nil {
			e.Target = target
		}
	case info.IsDir():
		e.Type = "dir"
	case info.Mode().IsRegular():
		e.Type = "file"
	default:
		e.Type = "other"
	}
	return e, nil
}

// heavyDirs is the default exclude list for tree traversal. Node modules
// and git objects are rarely what the caller wants and tend to explode the
// output size.
var heavyDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	".next":        true,
	".cache":       true,
	"vendor":       true,
}

// buildTree recursively walks a directory up to the configured depth.
// maxDepth == 0 means no limit.
func buildTree(path string, cur, maxDepth int, hidden, includeHeavy bool) (entry, bool, error) {
	e, err := statEntry(path)
	if err != nil {
		return entry{}, false, err
	}
	if e.Type != "dir" {
		return e, false, nil
	}
	if maxDepth != 0 && cur >= maxDepth {
		return e, true, nil
	}

	items, err := os.ReadDir(path)
	if err != nil {
		return e, false, err
	}

	truncated := false
	children := make([]entry, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !includeHeavy && heavyDirs[name] {
			continue
		}
		child, childTrunc, err := buildTree(filepath.Join(path, name), cur+1, maxDepth, hidden, includeHeavy)
		if err != nil {
			children = append(children, entry{Name: name, Type: "error", Mode: err.Error()})
			continue
		}
		if childTrunc {
			truncated = true
		}
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		if (children[i].Type == "dir") != (children[j].Type == "dir") {
			return children[i].Type == "dir"
		}
		return children[i].Name < children[j].Name
	})
	e.Children = children
	return e, truncated, nil
}

// optionalBool extracts an optional bool argument with a default value.
// mcp-go returns arguments as a map; missing keys yield the zero value.
func optionalBool(req mcp.CallToolRequest, key string, fallback bool) bool {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

// optionalInt extracts an optional int argument with a default value.
// JSON numbers arrive as float64 over the wire, so we convert explicitly.
func optionalInt(req mcp.CallToolRequest, key string, fallback int) int {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return fallback
}

// optionalString extracts an optional string argument with a default value.
func optionalString(req mcp.CallToolRequest, key string, fallback string) string {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

// optionalStringSlice extracts an optional []string argument. Useful for
// "paths", "patterns" and similar list arguments.
func optionalStringSlice(req mcp.CallToolRequest, key string) []string {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// formatSize makes byte counts readable in log output. Not used by the JSON
// responses (those stay numeric) but handy in error messages.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
