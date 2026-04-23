// Tool: meta — project_info, git_status, git_diff, stat, allowed_roots
//
// These are the tools that aren't in the upstream filesystem server but
// pay for themselves within the first session of real work. project_info
// answers "what am I looking at?"; git_status and git_diff plug me into
// the same mental model a human developer has.
//
// Git integration is implemented by shelling out to the git binary. This
// deliberately avoids pulling in go-git — git is always installed on a
// developer machine, and its CLI gives us canonical output.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerMeta(s *server.MCPServer, ctx *handlerCtx) {
	allowedTool := mcp.NewTool("allowed_roots",
		mcp.WithDescription("List the root directories this oosfs instance is allowed to access."),
		mcp.WithToolAnnotation(readOnlyAnnotations("Allowed roots")),
	)
	s.AddTool(allowedTool, ctx.handleAllowedRoots)

	statTool := mcp.NewTool("stat",
		mcp.WithDescription("Return detailed metadata for a single path (size, mtime, mode, symlink info, kind)."),
		mcp.WithToolAnnotation(readOnlyAnnotations("File metadata")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to inspect")),
	)
	s.AddTool(statTool, ctx.handleStat)

	projectTool := mcp.NewTool("project_info",
		mcp.WithDescription(
			"Detect project structure at the given path: git root, Go module, "+
				"package.json, pyproject.toml, Cargo.toml, Makefile, and more. "+
				"Call this once when entering an unfamiliar directory to orient.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Project info")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory to inspect")),
	)
	s.AddTool(projectTool, ctx.handleProjectInfo)

	gitStatusTool := mcp.NewTool("git_status",
		mcp.WithDescription("Run 'git status --porcelain=v1 -b' in the given directory and return parsed output."),
		mcp.WithToolAnnotation(readOnlyAnnotations("Git status")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory inside a git working tree")),
	)
	s.AddTool(gitStatusTool, ctx.handleGitStatus)

	gitDiffTool := mcp.NewTool("git_diff",
		mcp.WithDescription(
			"Run 'git diff' in the given directory. By default shows unstaged "+
				"changes; set staged=true for staged changes or 'rev' to diff "+
				"against an arbitrary ref.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Git diff")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory inside a git working tree")),
		mcp.WithBoolean("staged", mcp.Description("Show staged changes instead of unstaged (default: false)")),
		mcp.WithString("rev", mcp.Description("Optional ref to diff against (e.g. 'HEAD~1', 'main')")),
		mcp.WithString("pathspec", mcp.Description("Optional path filter relative to the repo (e.g. 'oos/core')")),
	)
	s.AddTool(gitDiffTool, ctx.handleGitDiff)
}

func (c *handlerCtx) handleAllowedRoots(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return jsonResult(map[string]any{
		"roots": c.reg.All(),
	}), nil
}

func (c *handlerCtx) handleStat(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("stat", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("stat", err), nil
	}
	e, err := statEntry(abs)
	if err != nil {
		return c.errResult("stat", err), nil
	}
	return jsonResult(map[string]any{
		"path": abs,
		"info": e,
	}), nil
}

// projectMarker describes one recognized project-structure signal.
type projectMarker struct {
	Kind   string `json:"kind"`              // "git" | "go-module" | "node" | "python" | ...
	File   string `json:"file,omitempty"`    // marker file that triggered detection
	Name   string `json:"name,omitempty"`    // module/project name if derivable
	Detail string `json:"details,omitempty"` // free-form extra info
}

func (c *handlerCtx) handleProjectInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("project_info", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("project_info", err), nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		return c.errResult("project_info", err), nil
	}
	dir := abs
	if !info.IsDir() {
		dir = filepath.Dir(abs)
	}

	markers := []projectMarker{}

	// Git root (walk up). If the root is an ancestor rather than the dir
	// itself, note that explicitly — the caller often wants to know
	// whether they're sitting inside a mono-repo vs. in a top-level repo.
	if root, branch := findGitRoot(dir); root != "" {
		detail := "branch=" + branch
		if root != dir {
			rel, _ := filepath.Rel(root, dir)
			detail += "; subdir=" + rel
		}
		markers = append(markers, projectMarker{
			Kind:   "git",
			File:   filepath.Join(root, ".git"),
			Detail: detail,
		})
	}

	// Go module (walk up for go.mod).
	if goMod, modPath := findGoModule(dir); goMod != "" {
		markers = append(markers, projectMarker{
			Kind: "go-module",
			File: goMod,
			Name: modPath,
		})
	}

	// Per-directory markers at the given path only.
	checks := []struct {
		file string
		kind string
	}{
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"Cargo.toml", "rust"},
		{"Makefile", "make"},
		{"setup.toml", "onisin"},
		{"oos.xsd", "onisin-dsl"},
	}
	for _, chk := range checks {
		p := filepath.Join(dir, chk.file)
		if _, err := os.Stat(p); err == nil {
			markers = append(markers, projectMarker{Kind: chk.kind, File: p})
		}
	}

	// Quick language breakdown based on extension counts, up to 2 levels
	// deep. This is cheap and usually right.
	langs := sampleLanguages(dir, 2)

	return jsonResult(map[string]any{
		"path":      abs,
		"directory": dir,
		"markers":   markers,
		"languages": langs,
	}), nil
}

func (c *handlerCtx) handleGitStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("git_status", err), nil
	}
	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("git_status", err), nil
	}
	dir := dirOf(abs)
	out, err := runGit(dir, "status", "--porcelain=v1", "-b")
	if err != nil {
		return c.errResult("git_status", err), nil
	}

	branch := ""
	entries := []map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			branch = strings.TrimPrefix(line, "## ")
			continue
		}
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		file := line[3:]
		entries = append(entries, map[string]string{
			"code": strings.TrimSpace(code),
			"file": file,
		})
	}
	return jsonResult(map[string]any{
		"dir":     dir,
		"branch":  branch,
		"entries": entries,
		"clean":   len(entries) == 0,
	}), nil
}

func (c *handlerCtx) handleGitDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("git_diff", err), nil
	}
	staged := optionalBool(req, "staged", false)
	rev := optionalString(req, "rev", "")
	pathspec := optionalString(req, "pathspec", "")

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("git_diff", err), nil
	}
	dir := dirOf(abs)

	args := []string{"diff", "--no-color"}
	if staged {
		args = append(args, "--staged")
	}
	if rev != "" {
		args = append(args, rev)
	}
	if pathspec != "" {
		args = append(args, "--", pathspec)
	}
	out, err := runGit(dir, args...)
	if err != nil {
		return c.errResult("git_diff", err), nil
	}
	return jsonResult(map[string]any{
		"dir":    dir,
		"args":   args,
		"diff":   out,
		"empty":  strings.TrimSpace(out) == "",
		"length": len(out),
	}), nil
}

// findGitRoot walks up from dir looking for a .git entry.
// Returns the root path and the current branch (best-effort).
func findGitRoot(dir string) (root, branch string) {
	cur := dir
	for {
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			_ = info
			root = cur
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ""
		}
		cur = parent
	}
	// Read HEAD to determine the branch without invoking git, which is faster
	// and avoids spawning a subprocess just to answer the common case.
	head, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err == nil {
		line := strings.TrimSpace(string(head))
		if strings.HasPrefix(line, "ref: refs/heads/") {
			branch = strings.TrimPrefix(line, "ref: refs/heads/")
		} else {
			branch = "DETACHED " + line
		}
	}
	return root, branch
}

// findGoModule locates the nearest go.mod and extracts the module path.
func findGoModule(dir string) (path, name string) {
	cur := dir
	for {
		candidate := filepath.Join(cur, "go.mod")
		if data, err := os.ReadFile(candidate); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					name = strings.TrimSpace(strings.TrimPrefix(line, "module"))
					break
				}
			}
			return candidate, name
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ""
		}
		cur = parent
	}
}

// sampleLanguages counts file extensions under dir up to maxDepth.
func sampleLanguages(dir string, maxDepth int) map[string]int {
	counts := map[string]int{}
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		if maxDepth > 0 && depth > maxDepth {
			return
		}
		items, err := os.ReadDir(p)
		if err != nil {
			return
		}
		for _, item := range items {
			name := item.Name()
			if strings.HasPrefix(name, ".") || heavyDirs[name] {
				continue
			}
			full := filepath.Join(p, name)
			if item.IsDir() {
				walk(full, depth+1)
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext == "" {
				continue
			}
			counts[ext]++
		}
	}
	walk(dir, 0)
	return counts
}

// runGit executes git in dir and returns stdout, capturing stderr in the error
// message if the command fails.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr))
	}
	return string(out), nil
}

// dirOf returns path if it's a directory, otherwise its parent. Convenient
// for git commands that must run inside a working tree.
func dirOf(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return filepath.Dir(path)
	}
	return path
}

// dumpJSON is kept unexported because it's only used in debug logging: it
// turns arbitrary structured values into compact JSON strings without
// erroring.
func dumpJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return string(data)
}
