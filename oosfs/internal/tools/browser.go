// Tool: browser — browser_open, browser_click, browser_fill,
//                 browser_text, browser_wait, browser_screenshot,
//                 browser_close
//
// Drives a local Chrome/Chromium via the Chrome DevTools Protocol
// (chromedp). A single browser instance is kept alive between calls so
// that state — cookies, scroll position, open tabs — persists the way a
// human would expect. The instance is started lazily on the first call
// and torn down either on browser_close or when oosfs exits.
//
// Profile data lives under ~/.oos/browser so the AI-driven browser is
// kept separate from the user's everyday Chrome. This avoids
// interference with real logins and keeps the session reproducible.
//
// Visibility (headless vs. headed) is decided per call. The default is
// headed — the user watches what happens. Switching visibility forces a
// browser restart because chromedp cannot toggle it on a live instance;
// the response makes the restart explicit so it is never a surprise.

package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// browserSession owns a live chromedp allocator + context pair.
// Exactly one is kept at a time per oosfs process.
type browserSession struct {
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	headless      bool
	remote        bool      // true = attached to external browser, false = spawned ourselves
	targetID      target.ID // the tab we own (remote mode); empty in exec mode
}

// close tears down the session. Safe to call on a nil receiver and
// safe to call multiple times.
func (b *browserSession) close() {
	if b == nil {
		return
	}
	if b.browserCancel != nil {
		b.browserCancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
}



// clearSingletonLocks removes the three lock files Chromium drops into a
// user-data-dir to guard against concurrent use. If a previous session
// crashed or was killed before it could clean up, these files linger and
// cause the next launch to silently exit. Since oosfs owns the profile
// directory entirely, it is safe to always sweep them on startup.
func clearSingletonLocks(profileDir string, logger *slog.Logger) {
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		p := filepath.Join(profileDir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			logger.Warn("could not remove stale lock", "path", p, "err", err)
		}
	}
}


// detectRemoteBrowser probes http://localhost:9222/json/version (the
// standard Chrome DevTools Protocol endpoint) and returns its
// webSocketDebuggerUrl when one is present. Returns "" on any failure
// — callers interpret that as "no remote browser available, start one
// ourselves". Timeout is intentionally tight; an unreachable endpoint
// must not slow down the common case where the user has no browser
// running.
func detectRemoteBrowser() string {
	if p := os.Getenv("OOSFS_BROWSER_WS"); p != "" {
		return p
	}
	port := os.Getenv("OOSFS_BROWSER_PORT")
	if port == "" {
		port = "9222"
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://localhost:" + port + "/json/version")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return v.WebSocketDebuggerURL
}

// pickBrowserBinary chooses which Chrome-family executable chromedp
// should drive. Standalone Chromium is preferred because it coexists
// cleanly with the user's everyday Chrome — Chrome refuses to run a
// second instance from a separate profile due to its allocator
// singleton, which blocks automation while the user is working.
//
// The OOSFS_BROWSER environment variable wins when set, then a list of
// known install locations, then an empty string to let chromedp fall
// back to whatever it finds on PATH. Callers treat "" as "no override".
func pickBrowserBinary() string {
	if p := os.Getenv("OOSFS_BROWSER"); p != "" {
		return p
	}
	candidates := []string{
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/opt/homebrew/bin/chromium",
		"/usr/local/bin/chromium",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// browserHandlerCtx wraps handlerCtx with the singleton session plus a
// mutex that serialises state transitions (start, restart, close).
// Tool calls themselves are cheap to serialise — a human-speed browser
// cannot meaningfully benefit from parallelism within a single session.
type browserHandlerCtx struct {
	*handlerCtx
	mu      sync.Mutex
	session *browserSession
	profileDir string
}

// registerBrowser wires the browser_* tools into the MCP server.
// Always registered — unlike the postgres tools, no config file is
// required. The user data directory is created on first use.
func registerBrowser(s *server.MCPServer, ctx *handlerCtx) {
	home, err := os.UserHomeDir()
	if err != nil {
		ctx.logger.Warn("browser tools not registered — home directory unknown", "err", err)
		return
	}
	profile := filepath.Join(home, ".oos", "browser")

	bc := &browserHandlerCtx{handlerCtx: ctx, profileDir: profile}
	ctx.logger.Info("browser tools enabled", "profile", profile)

	openTool := mcp.NewTool("browser_open",
		mcp.WithDescription(
			"Navigate the shared browser session to a URL and wait for the page to "+
				"reach DOMContentLoaded. Starts the browser on first use. "+
				"Default is a visible window so the user can watch; pass headless=true "+
				"for background work. Switching visibility restarts the browser and "+
				"loses any previous state — the response flags this explicitly.",
		),
		mcp.WithToolAnnotation(writeAnnotations("browser open")),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to navigate to")),
		mcp.WithBoolean("headless", mcp.Description("Run without a visible window (default: false)")),
	)
	s.AddTool(openTool, bc.handleOpen)

	clickTool := mcp.NewTool("browser_click",
		mcp.WithDescription(
			"Click the first element matching the CSS selector on the current page. "+
				"Waits up to 10 seconds for the element to become visible.",
		),
		mcp.WithToolAnnotation(writeAnnotations("browser click")),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector of the element to click")),
	)
	s.AddTool(clickTool, bc.handleClick)

	fillTool := mcp.NewTool("browser_fill",
		mcp.WithDescription(
			"Fill a form field identified by CSS selector with the given value. "+
				"Replaces any existing content. Waits up to 10 seconds for the field.",
		),
		mcp.WithToolAnnotation(writeAnnotations("browser fill")),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector of the input element")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Text to type into the field")),
	)
	s.AddTool(fillTool, bc.handleFill)

	textTool := mcp.NewTool("browser_text",
		mcp.WithDescription(
			"Return the visible text of an element, or of the whole page body when "+
				"no selector is given.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("browser text")),
		mcp.WithString("selector", mcp.Description("CSS selector; omit to read document.body")),
	)
	s.AddTool(textTool, bc.handleText)

	waitTool := mcp.NewTool("browser_wait",
		mcp.WithDescription(
			"Wait until an element matching the selector is visible. Useful for "+
				"pages that load content asynchronously. Timeout is 10 seconds.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("browser wait")),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector to wait for")),
	)
	s.AddTool(waitTool, bc.handleWait)

	screenshotTool := mcp.NewTool("browser_screenshot",
		mcp.WithDescription(
			"Capture a full-page PNG of the current page and return it as a base64 "+
				"data URL plus the byte size. Good for visual confirmation.",
		),
		mcp.WithToolAnnotation(readOnlyAnnotations("browser screenshot")),
	)
	s.AddTool(screenshotTool, bc.handleScreenshot)

	closeTool := mcp.NewTool("browser_close",
		mcp.WithDescription(
			"Close the browser session and free its resources. A subsequent "+
				"browser_open starts a fresh browser.",
		),
		mcp.WithToolAnnotation(destructiveAnnotations("browser close", true)),
	)
	s.AddTool(closeTool, bc.handleClose)
}

// ensureSession returns a live session with the requested visibility,
// starting or restarting the browser as needed. Caller must hold b.mu.
func (b *browserHandlerCtx) ensureSession(headless bool) (*browserSession, bool, error) {
	restarted := false
	if b.session != nil && b.session.headless != headless {
		b.logger.Info("browser restart — visibility change",
			"from_headless", b.session.headless, "to_headless", headless)
		b.session.close()
		b.session = nil
		restarted = true
	}
	if b.session != nil {
		return b.session, false, nil
	}

	// Remote mode: a Chromium started outside oosfs (via make browser
	// or the user's own invocation) exposes its DevTools endpoint over
	// TCP. Connecting to an existing browser is more reliable than
	// spawning one from inside a long-lived daemon — no subprocess
	// lifetime gymnastics, no macOS TCC surprises, and the user can
	// watch what we do in a real window they already trust.
	if wsURL := detectRemoteBrowser(); wsURL != "" {
		b.logger.Info("browser attach", "mode", "remote", "ws", wsURL)
		allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

		// Connect to the browser itself. This browser-level context is
		// what target.CreateTarget runs on. Do not wrap it in WithTimeout
		// for the setup calls — a cancelled timeout-child carries chromedp
		// state with it and breaks any tab context derived from the same
		// parent. We rely on the browser itself to be responsive here; if
		// Chromium hangs, the user notices and kills the window.
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		if err := chromedp.Run(browserCtx); err != nil {
			browserCancel()
			allocCancel()
			return nil, false, fmt.Errorf("connect to remote browser: %w", err)
		}

		// Create a fresh tab. The new tab exists independently from
		// whatever about:blank pages Chromium happened to have open, so
		// follow-up actions (click, fill, etc.) unambiguously hit it.
		var targetID target.ID
		if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			id, err := target.CreateTarget("about:blank").Do(ctx)
			if err != nil {
				return err
			}
			targetID = id
			return nil
		})); err != nil {
			browserCancel()
			allocCancel()
			return nil, false, fmt.Errorf("create tab: %w", err)
		}
		b.logger.Info("browser tab created", "target_id", string(targetID))

		// Rebind the session context to the new tab. Everything from here
		// on operates on that specific tab.
		tabCtx, tabCancel := chromedp.NewContext(browserCtx, chromedp.WithTargetID(targetID))
		b.session = &browserSession{
			allocCtx: allocCtx, allocCancel: allocCancel,
			browserCtx: tabCtx, browserCancel: func() { tabCancel(); browserCancel() },
			headless: headless,
			remote:   true,
			targetID: targetID,
		}
		return b.session, restarted, nil
	}
	b.logger.Info("browser attach", "mode", "exec")

	if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("create profile dir: %w", err)
	}
	clearSingletonLocks(b.profileDir, b.logger)

	// Start from DefaultExecAllocatorOptions but strip any pre-set
	// headless flag so we can set it ourselves without "allocator loaded
	// multiple times" complaints from the browser.
	base := make([]chromedp.ExecAllocatorOption, 0, len(chromedp.DefaultExecAllocatorOptions)+3)
	for _, o := range chromedp.DefaultExecAllocatorOptions {
		base = append(base, o)
	}
	if headless {
		base = append(base, chromedp.Headless)
	} else {
		// Explicit --no-headless is not a thing; the default allocator
		// sets Headless, so we cancel it by overriding after the fact.
		base = append(base, chromedp.Flag("headless", false))
	}
	opts := append(base,
		chromedp.UserDataDir(b.profileDir),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		// Force chromedp off its default pipe transport. On macOS with
		// codesigned Chromium the pipe handshake never completes — the
		// browser starts fine but chromedp cancels the context within a
		// second. WebSocket-on-TCP works reliably; chromedp picks a free
		// port itself when the pipe flag is explicitly disabled.
		chromedp.Flag("remote-debugging-pipe", false),
	)
	if bin := pickBrowserBinary(); bin != "" {
		b.logger.Info("browser binary", "path", bin)
		opts = append(opts, chromedp.ExecPath(bin))
	} else {
		b.logger.Info("browser binary", "path", "(chromedp default)")
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	// Surface what chromedp is doing internally while we still debug the
	// handshake. Set OOSFS_CHROME_DEBUG=1 to see every CDP message.
	var browserCtx context.Context
	var browserCancel context.CancelFunc
	if os.Getenv("OOSFS_CHROME_DEBUG") != "" {
		browserCtx, browserCancel = chromedp.NewContext(allocCtx,
			chromedp.WithLogf(func(format string, v ...any) {
				b.logger.Info("chromedp", "msg", fmt.Sprintf(format, v...))
			}),
			chromedp.WithDebugf(func(format string, v ...any) {
				b.logger.Debug("chromedp-wire", "msg", fmt.Sprintf(format, v...))
			}),
			chromedp.WithErrorf(func(format string, v ...any) {
				b.logger.Error("chromedp", "msg", fmt.Sprintf(format, v...))
			}),
		)
	} else {
		browserCtx, browserCancel = chromedp.NewContext(allocCtx)
	}

	// Force the browser to actually start now so errors surface
	// immediately rather than on the first navigation. 30s covers a cold
	// start with a fresh profile on macOS; the typical case is sub-5s.
	startCtx, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(startCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, false, fmt.Errorf("start chrome: %w", err)
	}

	b.session = &browserSession{
		allocCtx: allocCtx, allocCancel: allocCancel,
		browserCtx: browserCtx, browserCancel: browserCancel,
		headless: headless,
	}
	return b.session, restarted, nil
}

// runActions executes chromedp actions against the live browser context.
//
// Do NOT wrap sess.browserCtx in context.WithTimeout. chromedp binds the
// tab/target to the context you first Run on; cancelling a timeout-child
// tears that binding down, and every subsequent action on the session
// fails with "context canceled". Hanging pages are handled per-action
// (chromedp.WaitVisible has its own 10s timeout, Navigate waits for
// DOMContentLoaded with a built-in ceiling).
func (b *browserHandlerCtx) runActions(sess *browserSession, actions ...chromedp.Action) error {
	return chromedp.Run(sess.browserCtx, actions...)
}

func boolArg(req mcp.CallToolRequest, name string, def bool) bool {
	if raw, ok := req.GetArguments()[name]; ok {
		if v, ok := raw.(bool); ok {
			return v
		}
	}
	return def
}

func stringArg(req mcp.CallToolRequest, name string) string {
	if raw, ok := req.GetArguments()[name]; ok {
		if v, ok := raw.(string); ok {
			return v
		}
	}
	return ""
}


// activateTab brings our owned tab to the foreground. Chromium delivers
// click events to background tabs at the DOM level, but form submission
// (and anything that relies on the tab actually being the active one)
// stalls silently. Calling target.ActivateTarget before interactive
// actions matches what an IDE like WebStorm does with its remote Chrome
// driver. No-op in exec mode.
func (b *browserHandlerCtx) activateTab(sess *browserSession) error {
	if !sess.remote || sess.targetID == "" {
		return nil
	}
	return chromedp.Run(sess.browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return target.ActivateTarget(sess.targetID).Do(ctx)
	}))
}

func (b *browserHandlerCtx) handleOpen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url, err := req.RequireString("url")
	if err != nil {
		return b.errResult("browser_open", err), nil
	}
	headless := boolArg(req, "headless", false)

	b.mu.Lock()
	defer b.mu.Unlock()

	sess, restarted, err := b.ensureSession(headless)
	if err != nil {
		return b.errResult("browser_open", err), nil
	}
	if err := b.activateTab(sess); err != nil {
		return b.errResult("browser_open", fmt.Errorf("activate tab: %w", err)), nil
	}
	if err := b.runActions(sess, chromedp.Navigate(url)); err != nil {
		return b.errResult("browser_open", err), nil
	}
	return jsonResult(map[string]any{
		"url":       url,
		"headless":  headless,
		"restarted": restarted,
	}), nil
}

func (b *browserHandlerCtx) handleClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sel, err := req.RequireString("selector")
	if err != nil {
		return b.errResult("browser_click", err), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return b.errResult("browser_click", fmt.Errorf("no browser session — call browser_open first")), nil
	}
	if err := b.activateTab(b.session); err != nil {
		return b.errResult("browser_click", fmt.Errorf("activate tab: %w", err)), nil
	}
	if err := b.runActions(b.session,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Click(sel, chromedp.ByQuery),
	); err != nil {
		return b.errResult("browser_click", err), nil
	}
	return jsonResult(map[string]any{"clicked": sel}), nil
}

func (b *browserHandlerCtx) handleFill(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sel, err := req.RequireString("selector")
	if err != nil {
		return b.errResult("browser_fill", err), nil
	}
	value, err := req.RequireString("value")
	if err != nil {
		return b.errResult("browser_fill", err), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return b.errResult("browser_fill", fmt.Errorf("no browser session — call browser_open first")), nil
	}
	if err := b.runActions(b.session,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Clear(sel, chromedp.ByQuery),
		chromedp.SendKeys(sel, value, chromedp.ByQuery),
	); err != nil {
		return b.errResult("browser_fill", err), nil
	}
	return jsonResult(map[string]any{"filled": sel, "length": len(value)}), nil
}

func (b *browserHandlerCtx) handleText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sel := stringArg(req, "selector")
	if sel == "" {
		sel = "body"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return b.errResult("browser_text", fmt.Errorf("no browser session — call browser_open first")), nil
	}
	var text string
	if err := b.runActions(b.session,
		chromedp.Text(sel, &text, chromedp.ByQuery, chromedp.NodeVisible),
	); err != nil {
		return b.errResult("browser_text", err), nil
	}
	return jsonResult(map[string]any{"selector": sel, "text": text, "length": len(text)}), nil
}

func (b *browserHandlerCtx) handleWait(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sel, err := req.RequireString("selector")
	if err != nil {
		return b.errResult("browser_wait", err), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return b.errResult("browser_wait", fmt.Errorf("no browser session — call browser_open first")), nil
	}
	if err := b.runActions(b.session,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
	); err != nil {
		return b.errResult("browser_wait", err), nil
	}
	return jsonResult(map[string]any{"visible": sel}), nil
}

func (b *browserHandlerCtx) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return b.errResult("browser_screenshot", fmt.Errorf("no browser session — call browser_open first")), nil
	}
	var buf []byte
	if err := b.runActions(b.session,
		chromedp.FullScreenshot(&buf, 90),
	); err != nil {
		return b.errResult("browser_screenshot", err), nil
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf)
	return jsonResult(map[string]any{
		"bytes":    len(buf),
		"data_url": dataURL,
	}), nil
}

func (b *browserHandlerCtx) handleClose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return jsonResult(map[string]any{"closed": false, "reason": "no active session"}), nil
	}
	b.session.close()
	b.session = nil
	return jsonResult(map[string]any{"closed": true}), nil
}
