// Tool: exec_start / exec_read / exec_stop
//
// Streaming variant of exec for long-running commands: builds, test
// suites, servers that log while they run. Unlike exec, this variant
// returns immediately with a session ID; the caller then polls for new
// output via exec_read until the process finishes.
//
// Lifecycle:
//   exec_start  → session_id, pid
//   exec_read   → incremental stdout/stderr since last call + running flag
//   exec_stop   → kill the process (SIGTERM, then SIGKILL after 2s)
//
// Sessions are kept in an in-memory registry for the lifetime of the
// oosfs process. A stopped or finished session remains readable for a
// short grace period (10 minutes) so trailing output can still be
// collected, then it is evicted.

package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// sessionGracePeriod is how long a finished session lingers for reads
// before it is evicted. Ten minutes matches normal chat cadence — you can
// realistically come back to a build result within that window.
const sessionGracePeriod = 10 * time.Minute

// streamSession tracks one running (or recently-finished) command.
type streamSession struct {
	id        string
	command   string
	args      []string
	cwd       string
	startedAt time.Time
	finishedAt time.Time

	cmd     *exec.Cmd
	cancel  context.CancelFunc

	mu       sync.Mutex
	stdout   bytes.Buffer
	stderr   bytes.Buffer
	done     bool
	exitCode int
	runErr   error

	// Read positions: how much of the buffers has already been reported
	// to the caller. Allows exec_read to return only new data.
	stdoutPos int
	stderrPos int
}

// sessionRegistry holds all active and recently-finished streamSessions.
// Access is guarded by the registry mutex.
type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*streamSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{sessions: map[string]*streamSession{}}
}

func (r *sessionRegistry) add(s *streamSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.id] = s
}

func (r *sessionRegistry) get(id string) (*streamSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

func (r *sessionRegistry) reap() {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-sessionGracePeriod)
	for id, s := range r.sessions {
		s.mu.Lock()
		expired := s.done && s.finishedAt.Before(cutoff)
		s.mu.Unlock()
		if expired {
			delete(r.sessions, id)
		}
	}
}

// execStreamRegistry is package-global because there is exactly one MCP
// server per process. Guarded by its own mutex.
var execStreamRegistry = newSessionRegistry()

func registerExecStream(s *server.MCPServer, ctx *handlerCtx) {
	startTool := mcp.NewTool("exec_start",
		mcp.WithDescription(
			"Start a long-running command and return a session ID. Use "+
				"exec_read to poll output, exec_stop to terminate. Same command/args "+
				"semantics as exec; output is buffered in memory (no size cap — "+
				"callers pace themselves via exec_read).",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Start background command", false)),
		mcp.WithString("command", mcp.Required(), mcp.Description("Executable to run")),
		mcp.WithArray("args",
			mcp.Description("Command arguments"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("cwd", mcp.Required(), mcp.Description("Working directory inside an allowed root")),
		mcp.WithString("stdin", mcp.Description("Optional stdin data")),
		mcp.WithObject("env", mcp.Description("Extra environment variables")),
	)
	s.AddTool(startTool, ctx.handleExecStart)

	readTool := mcp.NewTool("exec_read",
		mcp.WithDescription(
			"Read new output from a streaming session since the last read. "+
				"Returns {stdout_delta, stderr_delta, running, exit_code}. "+
				"Poll until running=false to collect the full output.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("Read session output")),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("ID returned by exec_start")),
	)
	s.AddTool(readTool, ctx.handleExecRead)

	stopTool := mcp.NewTool("exec_stop",
		mcp.WithDescription(
			"Terminate a streaming session. Sends SIGTERM, escalates to "+
				"SIGKILL after 2 seconds if still running.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("Stop session", true)),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("ID returned by exec_start")),
	)
	s.AddTool(stopTool, ctx.handleExecStop)
}

func (c *handlerCtx) handleExecStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := req.RequireString("command")
	if err != nil {
		return c.errResult("exec_start", err), nil
	}
	cwdArg, err := req.RequireString("cwd")
	if err != nil {
		return c.errResult("exec_start", err), nil
	}
	args := optionalStringSlice(req, "args")
	stdin := optionalString(req, "stdin", "")
	extraEnv := optionalStringMap(req, "env")

	cwd, err := c.reg.Resolve(cwdArg)
	if err != nil {
		return c.errResult("exec_start", err), nil
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return c.errResult("exec_start", err), nil
	}
	if !info.IsDir() {
		return c.errResult("exec_start", fmt.Errorf("cwd %q is not a directory", cwd)), nil
	}

	resolvedCmd := command
	if !strings.ContainsRune(command, os.PathSeparator) {
		if p, lookErr := exec.LookPath(command); lookErr == nil {
			resolvedCmd = p
		}
	}

	// We use context.Background because the session outlives this MCP
	// call. Lifetime is governed by exec_stop or by the child exiting.
	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, resolvedCmd, args...)
	cmd.Dir = cwd
	cmd.Env = buildEnv(extraEnv)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	session := &streamSession{
		id:        newSessionID(),
		command:   resolvedCmd,
		args:      args,
		cwd:       cwd,
		startedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
	}
	cmd.Stdout = &syncedBuffer{buf: &session.stdout, mu: &session.mu}
	cmd.Stderr = &syncedBuffer{buf: &session.stderr, mu: &session.mu}

	if err := cmd.Start(); err != nil {
		cancel()
		return c.errResult("exec_start", err), nil
	}

	execStreamRegistry.add(session)
	execStreamRegistry.reap() // opportunistic cleanup

	// Wait in a goroutine so the session can complete without blocking
	// the MCP server.
	go func() {
		err := cmd.Wait()
		session.mu.Lock()
		defer session.mu.Unlock()
		session.done = true
		session.finishedAt = time.Now()
		session.runErr = err
		if err == nil {
			session.exitCode = 0
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				session.exitCode = exitErr.ExitCode()
			} else {
				session.exitCode = -1
			}
		}
	}()

	c.logger.Info("exec_start",
		"session_id", session.id,
		"command", resolvedCmd,
		"args", args,
		"cwd", cwd,
		"pid", cmd.Process.Pid,
	)

	return jsonResult(map[string]any{
		"session_id": session.id,
		"pid":        cmd.Process.Pid,
		"command":    resolvedCmd,
		"args":       args,
		"cwd":        cwd,
		"started_at": session.startedAt.UTC().Format(time.RFC3339),
	}), nil
}

func (c *handlerCtx) handleExecRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("session_id")
	if err != nil {
		return c.errResult("exec_read", err), nil
	}
	session, ok := execStreamRegistry.get(id)
	if !ok {
		return c.errResult("exec_read", fmt.Errorf("unknown session: %s", id)), nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	stdoutBytes := session.stdout.Bytes()
	stderrBytes := session.stderr.Bytes()
	stdoutDelta := string(stdoutBytes[session.stdoutPos:])
	stderrDelta := string(stderrBytes[session.stderrPos:])
	session.stdoutPos = len(stdoutBytes)
	session.stderrPos = len(stderrBytes)

	result := map[string]any{
		"session_id":    session.id,
		"running":       !session.done,
		"stdout_delta":  stdoutDelta,
		"stderr_delta":  stderrDelta,
		"stdout_total":  len(stdoutBytes),
		"stderr_total":  len(stderrBytes),
		"elapsed_ms":    time.Since(session.startedAt).Milliseconds(),
	}
	if session.done {
		result["exit_code"] = session.exitCode
		result["finished_at"] = session.finishedAt.UTC().Format(time.RFC3339)
		if session.runErr != nil {
			result["error"] = session.runErr.Error()
		}
	}
	return jsonResult(result), nil
}

func (c *handlerCtx) handleExecStop(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("session_id")
	if err != nil {
		return c.errResult("exec_stop", err), nil
	}
	session, ok := execStreamRegistry.get(id)
	if !ok {
		return c.errResult("exec_stop", fmt.Errorf("unknown session: %s", id)), nil
	}

	session.mu.Lock()
	alreadyDone := session.done
	proc := session.cmd.Process
	session.mu.Unlock()

	if alreadyDone {
		return jsonResult(map[string]any{
			"session_id": id,
			"status":     "already_finished",
			"exit_code":  session.exitCode,
		}), nil
	}

	// Polite first, then forceful.
	if proc != nil {
		_ = proc.Signal(os.Interrupt)
	}

	// Wait up to 2s for graceful shutdown.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		done := session.done
		session.mu.Unlock()
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	session.mu.Lock()
	stillRunning := !session.done
	session.mu.Unlock()
	if stillRunning {
		session.cancel() // CommandContext cancel -> SIGKILL
	}

	c.logger.Info("exec_stop", "session_id", id)
	return jsonResult(map[string]any{
		"session_id": id,
		"status":     "stopped",
	}), nil
}

// syncedBuffer is a tiny wrapper so stdout/stderr writes from the child
// process are serialized against reader access via the session mutex.
// Without this, a concurrent read while the child is writing is
// technically a data race on the underlying bytes.Buffer.
type syncedBuffer struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (s *syncedBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// newSessionID returns a short random hex ID. Eight bytes is plenty for
// the handful of concurrent sessions a single user would ever start.
func newSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Silence "imported but not used" in the unlikely case a future refactor
// removes the filepath import — kept as a documented safety net.
var _ = filepath.Separator
