// Package gopls drives a pool of long-lived gopls subprocesses and speaks
// just enough of the Language Server Protocol to answer hover, definition,
// references, documentSymbol and diagnostics queries from MCP tools.
//
// One client is spawned per workspace root (lazily, on first use) and
// kept alive for the lifetime of the oosfs process. gopls itself does the
// heavy lifting: package loading, type-checking, index building. The
// client only serializes requests and demultiplexes responses.
//
// Protocol framing follows the LSP spec — "Content-Length: N\r\n\r\n"
// followed by a UTF-8 JSON body. No third-party JSON-RPC library is used
// because the surface area needed is tiny and a hand-rolled reader is
// easier to debug.
package gopls

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Manager keeps one Client per workspace root. Clients are created on
// demand and never torn down before Close is called — gopls has a
// non-trivial startup cost for large modules, so we amortize it across
// the whole oosfs session.
type Manager struct {
	logger *slog.Logger

	mu      sync.Mutex
	clients map[string]*Client // key: absolute root path
}

// NewManager builds an empty Manager. The first call to For(...) for a
// given root will spawn gopls for that root.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:  logger,
		clients: map[string]*Client{},
	}
}

// For returns a ready-to-use Client for the given workspace root. The
// root must already have been resolved to an absolute path by the
// caller — Manager does no path canonicalization.
//
// On first call the gopls subprocess is started and the LSP handshake
// (initialize + initialized) is performed; subsequent calls reuse the
// same client. If startup fails, the error is returned and no client is
// cached, so the next call may retry.
func (m *Manager) For(ctx context.Context, root string) (*Client, error) {
	m.mu.Lock()
	if c, ok := m.clients[root]; ok {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()

	c, err := startClient(ctx, root, m.logger)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	// Re-check in case a concurrent caller beat us to it.
	if existing, ok := m.clients[root]; ok {
		m.mu.Unlock()
		_ = c.Close()
		return existing, nil
	}
	m.clients[root] = c
	m.mu.Unlock()
	return c, nil
}

// Close tears down every managed client. Safe to call multiple times.
func (m *Manager) Close() {
	m.mu.Lock()
	clients := m.clients
	m.clients = map[string]*Client{}
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

// Client is a single gopls subprocess wrapped in an LSP request/response
// pump. All public methods are safe for concurrent use.
type Client struct {
	root   string
	logger *slog.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader

	// writeMu serializes writes to stdin. LSP framing is stateful
	// (header + body must not interleave), so concurrent writes are
	// unsafe even if the pipe is.
	writeMu sync.Mutex

	// idMu guards nextID and pending.
	idMu    sync.Mutex
	nextID  int64
	pending map[int64]chan rawResponse

	// diag holds the latest diagnostics keyed by document URI. gopls
	// pushes these asynchronously via textDocument/publishDiagnostics
	// so we aggregate them here for the go_diagnostics tool.
	diagMu      sync.Mutex
	diagByURI   map[string][]Diagnostic
	diagVersion map[string]int

	// openDocs tracks which URIs we have already sent didOpen for.
	// didOpen on an already-open document is a protocol error.
	openMu   sync.Mutex
	openDocs map[string]bool

	closed   chan struct{}
	closeErr error
	closeMu  sync.Mutex
}

// rawResponse is the partially-decoded envelope of an LSP response.
// The Result is stored as raw JSON so the caller can unmarshal it into
// whatever concrete type it expects.
type rawResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *responseError) Error() string {
	return fmt.Sprintf("lsp error %d: %s", e.Code, e.Message)
}

// startClient launches gopls for a workspace root and performs the
// LSP initialize handshake. The returned client is ready for requests.
func startClient(ctx context.Context, root string, logger *slog.Logger) (*Client, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("root not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", root)
	}

	// "gopls serve" runs the LSP server on stdio. We rely on $PATH to
	// find the binary — the session handover notes gopls is installed
	// via Zed's debugger workflow, so $HOME/go/bin is expected on PATH.
	cmd := exec.Command("gopls", "serve")
	cmd.Dir = root
	// gopls writes its own logs to stderr; forward them to oosfs's
	// stderr to keep everything in one place.
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gopls (is it on $PATH?): %w", err)
	}

	c := &Client{
		root:        root,
		logger:      logger,
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		reader:      bufio.NewReader(stdout),
		pending:     map[int64]chan rawResponse{},
		diagByURI:   map[string][]Diagnostic{},
		diagVersion: map[string]int{},
		openDocs:    map[string]bool{},
		closed:      make(chan struct{}),
	}

	go c.readLoop()

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("lsp initialize: %w", err)
	}

	logger.Info("gopls started", "root", root)
	return c, nil
}

// Close shuts down the gopls subprocess. It's safe to call multiple
// times; only the first call does real work.
func (c *Client) Close() error {
	c.closeMu.Lock()
	select {
	case <-c.closed:
		c.closeMu.Unlock()
		return c.closeErr
	default:
	}
	close(c.closed)
	c.closeMu.Unlock()

	// Polite shutdown: shutdown then exit. Ignore errors — we're tearing
	// the process down anyway.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = c.call(ctx, "shutdown", nil)
	_ = c.notify("exit", nil)

	_ = c.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		c.closeErr = err
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
		c.closeErr = errors.New("gopls did not exit in time, killed")
	}
	return c.closeErr
}

// initialize runs the LSP handshake. Only a minimal set of client
// capabilities is advertised — enough to get useful replies for the
// five supported requests.
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   pathToURI(c.root),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover": map[string]any{
					"contentFormat": []string{"markdown", "plaintext"},
				},
				"definition":         map[string]any{"linkSupport": false},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"publishDiagnostics": map[string]any{},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
			},
		},
		"workspaceFolders": []map[string]any{
			{"uri": pathToURI(c.root), "name": filepath.Base(c.root)},
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// readLoop demultiplexes incoming LSP messages. Responses are dispatched
// to the matching pending channel; notifications (publishDiagnostics) are
// handled inline; everything else is logged and discarded.
func (c *Client) readLoop() {
	defer func() {
		// Fail any still-pending requests so callers don't hang
		// forever after gopls dies.
		c.idMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = nil
		c.idMu.Unlock()
	}()

	for {
		body, err := readMessage(c.reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.logger.Warn("gopls read", "err", err)
			}
			return
		}

		// Peek at "method" to decide notification vs. response.
		var peek struct {
			Method *string          `json:"method"`
			ID     *json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(body, &peek); err != nil {
			c.logger.Warn("gopls bad frame", "err", err, "body", truncate(string(body), 200))
			continue
		}

		switch {
		case peek.Method != nil && peek.ID == nil:
			c.handleNotification(*peek.Method, body)
		case peek.Method != nil && peek.ID != nil:
			// Server-to-client request. We don't handle any, but we
			// must reply with a "method not found" error to keep
			// gopls happy.
			c.replyMethodNotFound(body)
		case peek.ID != nil:
			var resp rawResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				c.logger.Warn("gopls bad response", "err", err)
				continue
			}
			c.idMu.Lock()
			ch, ok := c.pending[resp.ID]
			if ok {
				delete(c.pending, resp.ID)
			}
			c.idMu.Unlock()
			if ok {
				ch <- resp
				close(ch)
			}
		}
	}
}

// handleNotification processes server-initiated notifications. The only
// one we care about is publishDiagnostics.
func (c *Client) handleNotification(method string, body []byte) {
	switch method {
	case "textDocument/publishDiagnostics":
		var note struct {
			Params PublishDiagnosticsParams `json:"params"`
		}
		if err := json.Unmarshal(body, &note); err != nil {
			c.logger.Warn("gopls bad diagnostics", "err", err)
			return
		}
		c.diagMu.Lock()
		c.diagByURI[note.Params.URI] = note.Params.Diagnostics
		c.diagVersion[note.Params.URI]++
		c.diagMu.Unlock()
	case "window/showMessage", "window/logMessage":
		// Silently ignored; gopls is chatty and we don't want
		// those messages in the MCP logs.
	case "$/progress":
		// Workspace-loading progress. Ignored — callers wait for
		// concrete results instead.
	default:
		c.logger.Debug("gopls notification", "method", method)
	}
}

// replyMethodNotFound sends a -32601 error response to a server request.
// We don't need to handle any such requests, but silent dropping breaks
// some server features.
func (c *Client) replyMethodNotFound(body []byte) {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(req.ID),
		"error": map[string]any{
			"code":    -32601,
			"message": "method not found",
		},
	}
	_ = c.writeMessage(resp)
}

// call sends a request and waits for the matching response. Returns the
// raw result JSON so the caller can unmarshal it into a concrete type.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.idMu.Lock()
	if c.pending == nil {
		c.idMu.Unlock()
		return nil, errors.New("gopls client is closed")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rawResponse, 1)
	c.pending[id] = ch
	c.idMu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.writeMessage(msg); err != nil {
		c.idMu.Lock()
		delete(c.pending, id)
		c.idMu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("gopls closed mid-request")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.idMu.Lock()
		delete(c.pending, id)
		c.idMu.Unlock()
		return nil, ctx.Err()
	}
}

// notify sends a one-way notification (no response expected).
func (c *Client) notify(method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	return c.writeMessage(msg)
}

// writeMessage serializes v and writes it with LSP header framing.
func (c *Client) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal lsp message: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

// readMessage reads one LSP frame from the server.
func readMessage(r *bufio.Reader) ([]byte, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
			if err != nil {
				return nil, fmt.Errorf("bad content-length: %w", err)
			}
			contentLength = n
		}
		// Other headers (Content-Type) are ignored.
	}
	if contentLength <= 0 {
		return nil, errors.New("missing content-length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// EnsureOpen sends textDocument/didOpen for a file path if we haven't
// already. gopls requires this before hover/definition/references on
// that file. Repeated calls are cheap — the first wins, the rest are
// no-ops.
func (c *Client) EnsureOpen(ctx context.Context, absPath string) error {
	uri := pathToURI(absPath)
	c.openMu.Lock()
	already := c.openDocs[uri]
	c.openMu.Unlock()
	if already {
		return nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", absPath, err)
	}
	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "go",
			"version":    1,
			"text":       string(data),
		},
	}
	if err := c.notify("textDocument/didOpen", params); err != nil {
		return err
	}
	c.openMu.Lock()
	c.openDocs[uri] = true
	c.openMu.Unlock()
	return nil
}

// DiagnosticsFor returns the most recent diagnostics gopls has published
// for the given absolute file path. Because publishDiagnostics is an
// asynchronous server notification, this may return stale or empty
// results right after didOpen — callers that need fresh data should
// call WaitForDiagnostics instead.
func (c *Client) DiagnosticsFor(absPath string) []Diagnostic {
	uri := pathToURI(absPath)
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	diags := c.diagByURI[uri]
	out := make([]Diagnostic, len(diags))
	copy(out, diags)
	return out
}

// WaitForDiagnostics blocks until gopls publishes diagnostics for the
// given URI, or the context expires. It returns whatever is currently
// stored on timeout so the caller can still render a partial answer.
func (c *Client) WaitForDiagnostics(ctx context.Context, absPath string) []Diagnostic {
	uri := pathToURI(absPath)

	c.diagMu.Lock()
	startVersion := c.diagVersion[uri]
	c.diagMu.Unlock()

	deadline := time.NewTicker(50 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return c.DiagnosticsFor(absPath)
		case <-deadline.C:
			c.diagMu.Lock()
			current := c.diagVersion[uri]
			c.diagMu.Unlock()
			if current > startVersion {
				return c.DiagnosticsFor(absPath)
			}
		}
	}
}

// Root returns the absolute workspace root this client was created for.
func (c *Client) Root() string { return c.root }

// pathToURI converts an absolute filesystem path to a file:// URI.
// Only the spelling gopls expects is produced: no query, no fragment,
// hex-encoded spaces and unicode through the net/url encoder.
func pathToURI(p string) string {
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(p)}
	return u.String()
}

// uriToPath is the inverse of pathToURI. Invalid URIs yield the empty
// string — callers should treat that as "not a file I own".
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.FromSlash(u.Path)
}

// truncate shortens s to at most n runes for log output.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
