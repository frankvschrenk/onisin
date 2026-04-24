// Tool: meta — project_info, git_status, git_diff, git_commit, git_push,
//              stat, allowed_roots
//
// These are the tools that aren't in the upstream filesystem server but
// pay for themselves within the first session of real work. project_info
// answers "what am I looking at?"; git_status and git_diff plug me into
// the same mental model a human developer has; git_commit and git_push
// round out the end-to-end flow.
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

	gitCommitTool := mcp.NewTool("git_commit",
		mcp.WithDescription(
			"Stage changes, create a commit, and optionally push. If 'paths' "+
				"is omitted all tracked changes are staged ('git add -A'); "+
				"otherwise only the listed paths are staged. Set push=true to "+
				"push the resulting commit to the current branch's upstream.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Git commit", false)),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory inside a git working tree")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Commit message (may contain newlines)")),
		mcp.WithArray("paths",
			mcp.Description("Optional list of paths to stage; if empty, stages all tracked changes"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("push", mcp.Description("Push to the current branch's upstream after committing (default: false)")),
		mcp.WithBoolean("allow_empty", mcp.Description("Permit a commit that records no changes (default: false)")),
	)
	s.AddTool(gitCommitTool, ctx.handleGitCommit)

	gitPushTool := mcp.NewTool("git_push",
		mcp.WithDescription(
			"Push already-committed history to a remote. Does NOT create "+
				"a commit — use git_commit for that. By default pushes the "+
				"current branch to its upstream; if the branch has no "+
				"upstream yet and 'remote' is given, sets the upstream "+
				"(equivalent to 'git push -u <remote> <branch>'). "+
				"force_with_lease is available for history rewrites; "+
				"plain --force is deliberately not exposed.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Git push", false)),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory inside a git working tree")),
		mcp.WithString("remote", mcp.Description("Remote name (default: the branch's upstream, or 'origin' if none)")),
		mcp.WithString("branch", mcp.Description("Branch to push (default: current branch)")),
		mcp.WithBoolean("force_with_lease", mcp.Description("Use --force-with-lease to overwrite the remote branch safely (default: false)")),
		mcp.WithBoolean("tags", mcp.Description("Also push tags (--tags) (default: false)")),
	)
	s.AddTool(gitPushTool, ctx.handleGitPush)
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
		{"ctx.xsd", "onisin-ctx"},
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

	// Working-tree state. Cheap enough to always include when a git
	// root was found — callers routinely want "what branch, are we
	// clean, are we ahead of origin?" right alongside project detection.
	var gitState map[string]any
	if root, _ := findGitRoot(dir); root != "" {
		gitState = collectGitState(root)
	}

	resp := map[string]any{
		"path":      abs,
		"directory": dir,
		"markers":   markers,
		"languages": langs,
	}
	if gitState != nil {
		resp["git_state"] = gitState
	}
	return jsonResult(resp), nil
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

// handleGitCommit stages (optionally selected) paths, creates a commit, and
// optionally pushes it. Returns the new commit SHA, branch, and whether the
// push succeeded.
//
// Staging strategy: when 'paths' is empty, 'git add -A' is used so that
// deletions and new files are picked up just like a human running the same
// command. When 'paths' is given, only those entries are staged — useful
// for carving a multi-file working tree into several focused commits.
func (c *handlerCtx) handleGitCommit(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("git_commit", err), nil
	}
	message, err := req.RequireString("message")
	if err != nil {
		return c.errResult("git_commit", err), nil
	}
	if strings.TrimSpace(message) == "" {
		return c.errResult("git_commit", fmt.Errorf("message must not be empty")), nil
	}
	paths := optionalStringSlice(req, "paths")
	push := optionalBool(req, "push", false)
	allowEmpty := optionalBool(req, "allow_empty", false)

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("git_commit", err), nil
	}
	dir := dirOf(abs)

	// Stage. Explicit paths go through 'git add --' so filenames with
	// leading dashes are handled safely; the bulk path uses 'add -A' so
	// deletions are captured.
	if len(paths) == 0 {
		if _, err := runGit(dir, "add", "-A"); err != nil {
			return c.errResult("git_commit", err), nil
		}
	} else {
		args := append([]string{"add", "--"}, paths...)
		if _, err := runGit(dir, args...); err != nil {
			return c.errResult("git_commit", err), nil
		}
	}

	// Commit. --allow-empty is only added on request because the common
	// case — nothing staged — should surface as an error, not a silent
	// no-op commit.
	commitArgs := []string{"commit", "-m", message}
	if allowEmpty {
		commitArgs = append(commitArgs, "--allow-empty")
	}
	commitOut, err := runGit(dir, commitArgs...)
	if err != nil {
		return c.errResult("git_commit", err), nil
	}

	// Resolve the resulting SHA and branch for the caller. Both failures
	// are non-fatal: the commit already exists at this point, and the
	// caller can still work with the raw commit output.
	sha := ""
	if out, err := runGit(dir, "rev-parse", "HEAD"); err == nil {
		sha = strings.TrimSpace(out)
	}
	branch := ""
	if out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		branch = strings.TrimSpace(out)
	}

	result := map[string]any{
		"dir":     dir,
		"branch":  branch,
		"sha":     sha,
		"summary": strings.TrimSpace(commitOut),
		"pushed":  false,
	}

	if push {
		pushOut, err := runGit(dir, "push")
		if err != nil {
			// The commit is already in local history; bubble up the push
			// error without discarding the commit metadata.
			result["push_error"] = err.Error()
			return jsonResult(result), nil
		}
		result["pushed"] = true
		result["push_output"] = strings.TrimSpace(pushOut)
	}
	return jsonResult(result), nil
}

// handleGitPush pushes already-committed history to a remote. It is
// strictly a push — callers who also need to create a commit should use
// git_commit with push=true. Keeping the two tools separate makes it
// impossible to accidentally create an empty commit just to trigger a
// push, a failure mode that already bit us once.
//
// Default behaviour mirrors a plain 'git push' from the shell: push the
// current branch to its configured upstream. When 'remote' is given and
// the branch has no upstream yet, this tool sets the upstream on the
// first push ('git push -u <remote> <branch>') so that subsequent pushes
// can run without arguments. force_with_lease is exposed because it is
// the safe alternative to --force for legitimate history rewrites; plain
// --force is not exposed.
func (c *handlerCtx) handleGitPush(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return c.errResult("git_push", err), nil
	}
	remote := optionalString(req, "remote", "")
	branch := optionalString(req, "branch", "")
	forceWithLease := optionalBool(req, "force_with_lease", false)
	pushTags := optionalBool(req, "tags", false)

	abs, err := c.reg.Resolve(path)
	if err != nil {
		return c.errResult("git_push", err), nil
	}
	dir := dirOf(abs)

	// Resolve the current branch — needed both for the response and as
	// the default value when the caller didn't specify one.
	currentBranch := ""
	if out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		currentBranch = strings.TrimSpace(out)
	}
	if branch == "" {
		branch = currentBranch
	}

	// Detect whether the target branch already has an upstream. This
	// decides whether 'git push' without arguments would succeed, or
	// whether we need to set one up with -u.
	hasUpstream := false
	if branch != "" {
		if _, err := runGit(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{u}"); err == nil {
			hasUpstream = true
		}
	}

	args := []string{"push"}
	if forceWithLease {
		args = append(args, "--force-with-lease")
	}
	if pushTags {
		args = append(args, "--tags")
	}

	switch {
	case remote != "" && branch != "":
		// Explicit remote+branch. If no upstream is configured yet,
		// set it in the same push so future pushes work bare.
		if !hasUpstream {
			args = append(args, "-u")
		}
		args = append(args, remote, branch)
	case remote != "":
		// Remote without branch is an ambiguous request — the caller
		// probably meant "push current branch to this remote".
		if branch == "" {
			return c.errResult("git_push", fmt.Errorf("cannot determine current branch; specify 'branch' explicitly")), nil
		}
		if !hasUpstream {
			args = append(args, "-u")
		}
		args = append(args, remote, branch)
	default:
		// No remote given. If the branch has no upstream, fail loudly
		// rather than silently using a default — the caller needs to
		// decide.
		if !hasUpstream {
			return c.errResult(
				"git_push",
				fmt.Errorf("branch %q has no upstream; pass 'remote' (e.g. 'origin') to set one", branch),
			), nil
		}
	}

	out, err := runGit(dir, args...)
	if err != nil {
		return jsonResult(map[string]any{
			"dir":     dir,
			"branch":  currentBranch,
			"args":    args,
			"pushed":  false,
			"error":   err.Error(),
			"output":  strings.TrimSpace(out),
		}), nil
	}

	// Resolve the pushed SHA after the fact so the caller can correlate
	// the push with a specific commit. Best-effort: a resolve failure
	// here does not change the success state.
	sha := ""
	if s, err := runGit(dir, "rev-parse", "HEAD"); err == nil {
		sha = strings.TrimSpace(s)
	}

	return jsonResult(map[string]any{
		"dir":    dir,
		"branch": currentBranch,
		"sha":    sha,
		"args":   args,
		"pushed": true,
		"output": strings.TrimSpace(out),
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

// collectGitState returns a compact snapshot of a git working tree:
// branch, upstream, ahead/behind counts, and a clean flag. Callers use
// it through project_info to get working-tree orientation in the same
// round-trip as project structure detection.
//
// Parses 'git status --porcelain=v2 --branch', whose header lines are
// documented and stable:
//
//	# branch.oid  <sha>
//	# branch.head <name>              (or "(detached)")
//	# branch.upstream <name>          (absent if no upstream)
//	# branch.ab    +<ahead> -<behind>
//
// Any non-"#" line indicates a change, which alone is enough to flip
// clean=false without parsing the porcelain entry.
func collectGitState(root string) map[string]any {
	out, err := runGit(root, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	state := map[string]any{
		"root":  root,
		"clean": true,
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "# branch.head "):
			state["branch"] = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			state["upstream"] = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			ab := strings.TrimPrefix(line, "# branch.ab ")
			// Format: "+N -M". Leave as string on parse failure —
			// callers can still display the raw counter.
			var ahead, behind int
			if _, err := fmt.Sscanf(ab, "+%d -%d", &ahead, &behind); err == nil {
				state["ahead"] = ahead
				state["behind"] = behind
			} else {
				state["ab_raw"] = ab
			}
		case strings.HasPrefix(line, "#"):
			// Other header line (branch.oid, etc.) — ignored.
		default:
			// Any non-header line means the working tree has changes.
			state["clean"] = false
		}
	}
	return state
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
