// Tool: exec / which
//
// exec runs a command in one of the allowed roots and returns structured
// results (exit_code, stdout, stderr, duration). It is deliberately
// unrestricted: no command allowlist, no git-write guard. oosfs trusts
// whoever started it and assumes the user knows what they're doing.
//
// Design choices:
//   - No shell interpretation. exec.Command(cmd, args...) means no
//     injection via metacharacters. For pipes or redirection the caller
//     passes ["sh", "-c", "..."] explicitly — and then they own the risk.
//   - cwd must live inside an allowed root. This is the one hard rule.
//   - Output caps are soft: large stdout/stderr is truncated with a flag,
//     never silently dropped. Prevents a noisy `go test -v` from burning
//     the whole context window.
//   - Timeout defaults to 60s but is overridable up to 1h. Longer tasks
//     should run outside the MCP round-trip anyway.
//   - Every exec emits an audit log line through slog so the user can
//     grep what the LLM did after the fact.
//
// which looks up an executable in $PATH — tiny, read-only, used to
// decide whether a tool is available before calling exec.

package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// defaultExecTimeout is used when the caller does not specify one.
// Generous enough for a fresh `go build`, short enough that an infinite
// loop gets caught.
const defaultExecTimeout = 60 * time.Second

// maxExecTimeout is the upper bound for a single exec call. Jobs that
// need longer should run outside oosfs (tmux, a CI system, or whatever
// the user normally uses).
const maxExecTimeout = 60 * time.Minute

// maxOutputBytes caps each of stdout and stderr independently. A
// truncation marker is appended so the LLM knows output was cut.
const maxOutputBytes = 1 << 20 // 1 MiB

// execResult is the JSON payload returned by exec. Field order mirrors
// what a human would want to see first: did it work, what did it print,
// how long did it take.
type execResult struct {
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	CWD         string   `json:"cwd"`
	ExitCode    int      `json:"exit_code"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	DurationMS  int64    `json:"duration_ms"`
	TimedOut    bool     `json:"timed_out,omitempty"`
	Truncated   bool     `json:"truncated,omitempty"`
	TruncStdout bool     `json:"stdout_truncated,omitempty"`
	TruncStderr bool     `json:"stderr_truncated,omitempty"`
	Error       string   `json:"error,omitempty"`
}

func registerExec(s *server.MCPServer, ctx *handlerCtx) {
	execTool := mcp.NewTool("exec",
		mcp.WithDescription(
			"Run a command in an allowed directory and return stdout, stderr, "+
				"and exit code. No shell interpretation: pass the binary in "+
				"'command' and arguments separately in 'args'. For shell-style "+
				"pipes or redirection, pass command='sh' and args=['-c', '...']. "+
				"Default timeout 60s, max 3600s. Output is capped at 1 MiB per "+
				"stream with a truncation flag.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Execute command", false)),
		mcp.WithString("command", mcp.Required(), mcp.Description("Executable to run (looked up in $PATH or as an absolute path)")),
		mcp.WithArray("args",
			mcp.Description("Command arguments as a list of strings"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("cwd", mcp.Required(), mcp.Description("Working directory — must be inside an allowed root")),
		mcp.WithNumber("timeout_seconds", mcp.Description("Timeout in seconds (default: 60, max: 3600)")),
		mcp.WithString("stdin", mcp.Description("Optional data to feed on the process's standard input")),
		mcp.WithObject("env",
			mcp.Description("Additional environment variables as {KEY: VALUE}. Merged on top of a minimal PATH/HOME/USER/LANG base."),
		),
	)
	s.AddTool(execTool, ctx.handleExec)

	whichTool := mcp.NewTool("which",
		mcp.WithDescription(
			"Resolve an executable name against $PATH. Returns the absolute "+
				"path if found, or an error if not. Useful to check tool "+
				"availability before calling exec.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Locate executable")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Executable name (e.g. 'go', 'git', 'rg')")),
	)
	s.AddTool(whichTool, ctx.handleWhich)
}

func (c *handlerCtx) handleWhich(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return c.errResult("which", err), nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return jsonResult(map[string]any{
			"name":  name,
			"found": false,
			"error": err.Error(),
		}), nil
	}
	abs, _ := filepath.Abs(p)
	return jsonResult(map[string]any{
		"name":  name,
		"found": true,
		"path":  abs,
	}), nil
}

func (c *handlerCtx) handleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := req.RequireString("command")
	if err != nil {
		return c.errResult("exec", err), nil
	}
	cwdArg, err := req.RequireString("cwd")
	if err != nil {
		return c.errResult("exec", err), nil
	}

	args := optionalStringSlice(req, "args")
	stdin := optionalString(req, "stdin", "")
	timeout := resolveTimeout(optionalInt(req, "timeout_seconds", 0))
	extraEnv := optionalStringMap(req, "env")

	cwd, err := c.reg.Resolve(cwdArg)
	if err != nil {
		return c.errResult("exec", err), nil
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return c.errResult("exec", err), nil
	}
	if !info.IsDir() {
		return c.errResult("exec", fmt.Errorf("cwd %q is not a directory", cwd)), nil
	}

	// Resolve the command to an absolute path up front so the audit log
	// records what actually ran, not just what was asked for. If the
	// command contains a path separator we trust the caller's absolute or
	// relative spec.
	resolvedCmd := command
	if !strings.ContainsRune(command, os.PathSeparator) {
		if p, lookErr := exec.LookPath(command); lookErr == nil {
			resolvedCmd = p
		}
	}

	c.logger.Info("exec",
		"command", resolvedCmd,
		"args", args,
		"cwd", cwd,
		"timeout_s", int(timeout.Seconds()),
	)

	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, resolvedCmd, args...)
	cmd.Dir = cwd
	cmd.Env = buildEnv(extraEnv)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr capBuffer
	stdout.limit = maxOutputBytes
	stderr.limit = maxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	result := execResult{
		Command:     resolvedCmd,
		Args:        args,
		CWD:         cwd,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		DurationMS:  duration.Milliseconds(),
		TruncStdout: stdout.truncated,
		TruncStderr: stderr.truncated,
		Truncated:   stdout.truncated || stderr.truncated,
	}

	// Classify the run outcome.
	switch {
	case runErr == nil:
		result.ExitCode = 0
	case errors.Is(ctxTimeout.Err(), context.DeadlineExceeded):
		result.TimedOut = true
		result.ExitCode = -1
		result.Error = fmt.Sprintf("timed out after %s", timeout)
	default:
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Startup errors: command not found, permission denied, etc.
			result.ExitCode = -1
			result.Error = runErr.Error()
		}
	}

	c.logger.Info("exec done",
		"command", resolvedCmd,
		"cwd", cwd,
		"exit_code", result.ExitCode,
		"duration_ms", result.DurationMS,
		"timed_out", result.TimedOut,
	)

	return jsonResult(result), nil
}

// resolveTimeout clamps the caller's request to the legal range.
// A zero or negative value picks the default.
func resolveTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultExecTimeout
	}
	t := time.Duration(seconds) * time.Second
	if t > maxExecTimeout {
		return maxExecTimeout
	}
	return t
}

// buildEnv assembles the environment for a child process.
//
// We start from a minimal safe base (PATH, HOME, USER, LANG, LC_*, SHELL,
// TMPDIR) so secrets leaking in via the parent environment are not
// automatically handed to every subprocess. Callers can layer anything
// else on top via the 'env' argument.
var passthroughEnv = []string{
	"PATH",
	"HOME",
	"USER",
	"LOGNAME",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
	"SHELL",
	"TMPDIR",
	"GOPATH",
	"GOROOT",
	"GOFLAGS",
	"GOCACHE",
	"GOMODCACHE",
}

func buildEnv(extra map[string]string) []string {
	env := make([]string, 0, len(passthroughEnv)+len(extra))
	for _, key := range passthroughEnv {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// capBuffer is a bytes.Buffer that stops accepting data after 'limit'
// bytes and records that a truncation happened. We implement this rather
// than reaching for io.LimitWriter because we want to distinguish
// "finished naturally" from "cut short" in the output JSON.
type capBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if c.truncated {
		// Pretend we accepted the write so the child process doesn't
		// receive a broken pipe and die early. We just drop the bytes.
		return len(p), nil
	}
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	n, err := c.buf.Write(p[:remaining])
	if err == nil {
		c.truncated = true
		// Append a visible marker so the consumer sees the cut in-stream.
		_, _ = c.buf.WriteString("\n…[output truncated]…\n")
		return len(p), nil
	}
	return n, err
}

func (c *capBuffer) String() string { return c.buf.String() }

// optionalStringMap extracts an optional map[string]string argument.
// Used for the 'env' parameter where values are expected to be strings.
func optionalStringMap(req mcp.CallToolRequest, key string) map[string]string {
	args := req.GetArguments()
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

// drainAll is a small helper kept around for the future streaming exec
// variant: it reads everything from r and returns the content plus
// whether truncation occurred. Currently unused — kept so the streaming
// path has a shared primitive ready when we add it.
func drainAll(r io.Reader, limit int) (string, bool, error) {
	var buf bytes.Buffer
	n, err := io.CopyN(&buf, r, int64(limit))
	if err == io.EOF {
		return buf.String(), false, nil
	}
	if err != nil {
		return buf.String(), false, err
	}
	// If we successfully read exactly 'limit' bytes, there might be more.
	if n == int64(limit) {
		// Peek one more byte to know for sure.
		var probe [1]byte
		m, _ := r.Read(probe[:])
		if m > 0 {
			return buf.String(), true, nil
		}
	}
	return buf.String(), false, nil
}
