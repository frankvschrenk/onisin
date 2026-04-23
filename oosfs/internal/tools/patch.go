// Tool: apply_patch
//
// Applies a unified diff to files inside an allowed root. This is the
// natural complement to `edit`: edit is for a single find/replace,
// apply_patch handles multi-location or multi-file changes in one go.
//
// Implementation uses `git apply` under the hood, which is the canonical
// unified-diff applier. Benefits:
//   - Handles context-based matching (fuzzy within a few lines).
//   - Supports adding, deleting, and renaming files.
//   - Validates the patch cleanly applies before touching anything when
//     --check is used.
//   - Works even outside a git repo thanks to --directory.
//
// The tool requires at least one allowed root to contain the target
// files, and git apply runs with CWD set to the first allowed root that
// contains the paths (or the user-supplied cwd).

package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerPatch(s *server.MCPServer, ctx *handlerCtx) {
	tool := mcp.NewTool("apply_patch",
		mcp.WithDescription(
			"Apply a unified diff (git format) to files in an allowed root. "+
				"Handles multi-file and multi-hunk patches atomically: if any "+
				"hunk fails to apply, nothing is written. Set check=true to "+
				"only validate without applying. Paths in the patch headers "+
				"(a/..., b/...) are resolved relative to cwd.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Apply patch", true)),
		mcp.WithString("patch", mcp.Required(), mcp.Description("Unified diff content (same format as `git diff` output)")),
		mcp.WithString("cwd", mcp.Required(), mcp.Description("Directory where paths in the patch are rooted — must be inside an allowed root")),
		mcp.WithBoolean("check", mcp.Description("Only check if the patch applies, don't write (default: false)")),
		mcp.WithNumber("strip", mcp.Description("Number of leading path components to strip, like patch -pN (default: 1)")),
	)
	s.AddTool(tool, ctx.handleApplyPatch)
}

func (c *handlerCtx) handleApplyPatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	patch, err := req.RequireString("patch")
	if err != nil {
		return c.errResult("apply_patch", err), nil
	}
	cwdArg, err := req.RequireString("cwd")
	if err != nil {
		return c.errResult("apply_patch", err), nil
	}
	check := optionalBool(req, "check", false)
	strip := optionalInt(req, "strip", 1)

	cwd, err := c.reg.Resolve(cwdArg)
	if err != nil {
		return c.errResult("apply_patch", err), nil
	}

	// Always run a check pass first so we know whether the patch is clean.
	// If the caller wanted check-only, we stop here.
	checkOut, checkErr := runGitApply(ctx, cwd, patch, strip, true)
	if checkErr != nil {
		return jsonResult(map[string]any{
			"cwd":     cwd,
			"applied": false,
			"check":   true,
			"error":   checkErr.Error(),
			"output":  checkOut,
		}), nil
	}
	if check {
		return jsonResult(map[string]any{
			"cwd":     cwd,
			"applied": false,
			"check":   true,
			"status":  "patch applies cleanly",
		}), nil
	}

	// Apply for real.
	applyOut, applyErr := runGitApply(ctx, cwd, patch, strip, false)
	if applyErr != nil {
		return jsonResult(map[string]any{
			"cwd":     cwd,
			"applied": false,
			"error":   applyErr.Error(),
			"output":  applyOut,
		}), nil
	}

	c.logger.Info("apply_patch", "cwd", cwd, "bytes", len(patch), "strip", strip)
	return jsonResult(map[string]any{
		"cwd":     cwd,
		"applied": true,
		"bytes":   len(patch),
		"output":  applyOut,
	}), nil
}

// runGitApply shells out to `git apply` with the patch fed via stdin.
// Using --check for validation and plain apply for the write pass.
func runGitApply(ctx context.Context, cwd, patch string, strip int, checkOnly bool) (string, error) {
	args := []string{"apply", "--whitespace=nowarn", fmt.Sprintf("-p%d", strip)}
	if checkOnly {
		args = append(args, "--check")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(patch)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if s := stderr.String(); s != "" {
		if output != "" {
			output += "\n"
		}
		output += s
	}
	if err != nil {
		return output, fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
