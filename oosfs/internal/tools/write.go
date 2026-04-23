// Tool: write / append / edit / mkdir / move / copy / remove
//
// The edit tool is intentionally simple and atomic: you provide an exact
// string to find and a replacement. The tool verifies uniqueness (or the
// expected number of occurrences) before writing. This is a direct response
// to the edit_file unreliability noted in the oos work log.
//
// Write operations always go through an atomic temp-file + rename to avoid
// ending up with half-written files if something crashes mid-write.

package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerWrite(s *server.MCPServer, ctx *handlerCtx) {
	writeTool := mcp.NewTool("write",
		mcp.WithDescription(
			"Create a new file or overwrite an existing one atomically. Parent "+
				"directories are created as needed.",
		),
		mcp.WithToolAnnotation(writeAnnotations("Write file")),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Full file content")),
	)
	s.AddTool(writeTool, ctx.handleWrite)

	appendTool := mcp.NewTool("append",
		mcp.WithDescription("Append text to an existing file (or create it if missing)."),
		// Append is destructive in the MCP sense (it mutates the file) AND
		// not idempotent — repeated calls grow the file.
		mcp.WithToolAnnotation(destructiveAnnotations("Append to file", false)),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Text to append")),
	)
	s.AddTool(appendTool, ctx.handleAppend)

	editTool := mcp.NewTool("edit",
		mcp.WithDescription(
			"Replace occurrences of 'find' with 'replace' in a file. By default "+
				"requires 'find' to occur exactly once, which avoids silent "+
				"multi-replace surprises. Set expect_count=N to require exactly N "+
				"matches, or expect_count=-1 to allow any positive count. Use "+
				"dry_run=true to preview the diff without writing.",
		),
		// Edit mutates a file but is idempotent when find/replace are fixed.
		mcp.WithToolAnnotation(destructiveAnnotations("Edit file", true)),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithString("find", mcp.Required(), mcp.Description("Exact string to find")),
		mcp.WithString("replace", mcp.Required(), mcp.Description("Replacement string")),
		mcp.WithNumber("expect_count", mcp.Description("Required number of matches (default: 1, -1 = any)")),
		mcp.WithBoolean("dry_run", mcp.Description("Return diff preview without writing (default: false)")),
	)
	s.AddTool(editTool, ctx.handleEdit)

	mkdirTool := mcp.NewTool("mkdir",
		mcp.WithDescription("Create a directory and all needed parents."),
		// MkdirAll is idempotent and non-destructive.
		mcp.WithToolAnnotation(writeAnnotations("Create directory")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory path")),
	)
	s.AddTool(mkdirTool, ctx.handleMkdir)

	moveTool := mcp.NewTool("move",
		mcp.WithDescription("Move or rename a file or directory. Both paths must live within allowed roots."),
		// Move can clobber the destination; treat as destructive, not idempotent.
		mcp.WithToolAnnotation(destructiveAnnotations("Move / rename", false)),
		mcp.WithString("src", mcp.Required(), mcp.Description("Source path")),
		mcp.WithString("dst", mcp.Required(), mcp.Description("Destination path")),
	)
	s.AddTool(moveTool, ctx.handleMove)

	copyTool := mcp.NewTool("copy",
		mcp.WithDescription("Copy a file (single file only; use a recursive tool later for directories)."),
		// Copy overwrites the destination — destructive, not idempotent.
		mcp.WithToolAnnotation(destructiveAnnotations("Copy file", false)),
		mcp.WithString("src", mcp.Required(), mcp.Description("Source file")),
		mcp.WithString("dst", mcp.Required(), mcp.Description("Destination file")),
	)
	s.AddTool(copyTool, ctx.handleCopy)

	removeTool := mcp.NewTool("remove",
		mcp.WithDescription(
			"Delete a file or empty directory. Set recursive=true to delete "+
				"directories with contents. There is no trash can — this is final.",
		),
		// Remove is the canonical destructive operation.
		mcp.WithToolAnnotation(destructiveAnnotations("Delete file or directory", true)),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to delete")),
		mcp.WithBoolean("recursive", mcp.Description("Allow recursive directory removal (default: false)")),
	)
	s.AddTool(removeTool, ctx.handleRemove)
}

func (c *handlerCtx) handleWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("write", err), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return c.errResult("write", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("write", err), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return c.errResult("write", err), nil
	}
	if err := atomicWrite(abs, []byte(content)); err != nil {
		return c.errResult("write", err), nil
	}
	return jsonResult(map[string]any{
		"path":    abs,
		"bytes":   len(content),
		"status":  "ok",
		"created": !fileExists(abs) || false, // see note below
	}), nil
}

func (c *handlerCtx) handleAppend(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("append", err), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return c.errResult("append", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("append", err), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return c.errResult("append", err), nil
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return c.errResult("append", err), nil
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return c.errResult("append", err), nil
	}
	return jsonResult(map[string]any{
		"path":          abs,
		"bytes_written": n,
		"status":        "ok",
	}), nil
}

func (c *handlerCtx) handleEdit(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("edit", err), nil
	}
	find, err := req.RequireString("find")
	if err != nil {
		return c.errResult("edit", err), nil
	}
	replace, err := req.RequireString("replace")
	if err != nil {
		return c.errResult("edit", err), nil
	}
	expectCount := optionalInt(req, "expect_count", 1)
	dryRun := optionalBool(req, "dry_run", false)

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("edit", err), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return c.errResult("edit", err), nil
	}
	original := string(data)

	count := strings.Count(original, find)
	switch {
	case expectCount == -1:
		if count == 0 {
			return c.errResult("edit", fmt.Errorf("no occurrences of 'find' in %s", abs)), nil
		}
	case count != expectCount:
		return c.errResult("edit", fmt.Errorf(
			"expected %d occurrences of 'find' in %s but got %d",
			expectCount, abs, count)), nil
	}

	updated := strings.ReplaceAll(original, find, replace)
	diff := makeDiff(abs, original, updated)

	if dryRun {
		return jsonResult(map[string]any{
			"path":         abs,
			"dry_run":      true,
			"replacements": count,
			"diff":         diff,
		}), nil
	}

	if err := atomicWrite(abs, []byte(updated)); err != nil {
		return c.errResult("edit", err), nil
	}
	return jsonResult(map[string]any{
		"path":         abs,
		"replacements": count,
		"diff":         diff,
		"status":       "ok",
	}), nil
}

func (c *handlerCtx) handleMkdir(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("mkdir", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("mkdir", err), nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return c.errResult("mkdir", err), nil
	}
	return jsonResult(map[string]any{"path": abs, "status": "ok"}), nil
}

func (c *handlerCtx) handleMove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	src, err := req.RequireString("src")
	if err != nil {
		return c.errResult("move", err), nil
	}
	dst, err := req.RequireString("dst")
	if err != nil {
		return c.errResult("move", err), nil
	}
	srcAbs, err := c.reg.Resolve(src)
	if err != nil {
		return c.errResult("move", err), nil
	}
	dstAbs, err := c.reg.Resolve(dst)
	if err != nil {
		return c.errResult("move", err), nil
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return c.errResult("move", err), nil
	}
	if err := os.Rename(srcAbs, dstAbs); err != nil {
		return c.errResult("move", err), nil
	}
	return jsonResult(map[string]any{"src": srcAbs, "dst": dstAbs, "status": "ok"}), nil
}

func (c *handlerCtx) handleCopy(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	src, err := req.RequireString("src")
	if err != nil {
		return c.errResult("copy", err), nil
	}
	dst, err := req.RequireString("dst")
	if err != nil {
		return c.errResult("copy", err), nil
	}
	srcAbs, err := c.reg.Resolve(src)
	if err != nil {
		return c.errResult("copy", err), nil
	}
	dstAbs, err := c.reg.Resolve(dst)
	if err != nil {
		return c.errResult("copy", err), nil
	}

	info, err := os.Stat(srcAbs)
	if err != nil {
		return c.errResult("copy", err), nil
	}
	if info.IsDir() {
		return c.errResult("copy", fmt.Errorf("copy does not yet support directories")), nil
	}

	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return c.errResult("copy", err), nil
	}
	in, err := os.Open(srcAbs)
	if err != nil {
		return c.errResult("copy", err), nil
	}
	defer in.Close()
	out, err := os.Create(dstAbs)
	if err != nil {
		return c.errResult("copy", err), nil
	}
	defer out.Close()
	n, err := io.Copy(out, in)
	if err != nil {
		return c.errResult("copy", err), nil
	}
	return jsonResult(map[string]any{"src": srcAbs, "dst": dstAbs, "bytes": n, "status": "ok"}), nil
}

func (c *handlerCtx) handleRemove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("remove", err), nil
	}
	recursive := optionalBool(req, "recursive", false)
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("remove", err), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return c.errResult("remove", err), nil
	}
	if info.IsDir() && !recursive {
		if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
			return c.errResult("remove", fmt.Errorf("directory not empty, pass recursive=true to delete")), nil
		}
	}
	if recursive {
		if err := os.RemoveAll(abs); err != nil {
			return c.errResult("remove", err), nil
		}
	} else {
		if err := os.Remove(abs); err != nil {
			return c.errResult("remove", err), nil
		}
	}
	return jsonResult(map[string]any{"path": abs, "status": "ok"}), nil
}

// atomicWrite writes data to path via a temporary file in the same directory
// and then renames it. On Unix this is atomic for ordinary files.
//
// File permissions are preserved: if path already exists, its mode is read
// before the write and re-applied to the new file. For new files, the mode
// defaults to 0644 (umask applies). This avoids the common bug where an
// atomic write via os.CreateTemp leaves the target with the temp file's
// 0600 permissions.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)

	// Remember the original mode if the file already exists.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".oosfs-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	// Apply the intended mode before rename so the switch is atomic.
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// makeDiff produces a compact summary: count of added/removed lines plus
// the position of the first difference. A full unified diff would be
// bigger than useful for typical LLM-driven edits.
//
// Line counting matches the convention used by `read`: a trailing newline
// does not add an empty line. That way `lines_before` and the `total_lines`
// reported by `read` agree for the same file.
func makeDiff(path, before, after string) map[string]any {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)

	added := 0
	removed := 0
	// Rough line-level diff: count first-level mismatch positions. For a
	// real diff we'd pull in a diff library, but for edit previews this
	// gives the LLM enough signal.
	maxLen := len(beforeLines)
	if len(afterLines) > maxLen {
		maxLen = len(afterLines)
	}
	firstDiff := -1
	for i := 0; i < maxLen; i++ {
		b, a := "", ""
		bExists := i < len(beforeLines)
		aExists := i < len(afterLines)
		if bExists {
			b = beforeLines[i]
		}
		if aExists {
			a = afterLines[i]
		}
		if b == a && bExists == aExists {
			continue
		}
		if firstDiff == -1 {
			firstDiff = i + 1
		}
		if bExists {
			removed++
		}
		if aExists {
			added++
		}
	}
	return map[string]any{
		"path":            path,
		"lines_before":    len(beforeLines),
		"lines_after":     len(afterLines),
		"added_lines":     added,
		"removed_lines":   removed,
		"first_diff_line": firstDiff,
	}
}

// splitLines splits s into lines, dropping at most one trailing empty line
// produced by a final newline character. This aligns counts with how a
// human reads the file ("N lines") rather than how strings.Split sees it.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// fileExists is a tiny helper used in the "created" field of write results.
// The current implementation always reports false; kept here as a hook for
// a future "did the file exist before?" signal without widening the public
// interface of write.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
