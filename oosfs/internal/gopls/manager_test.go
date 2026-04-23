package gopls

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// TestManagerSmoke spawns a real gopls against the oosfs module itself and
// exercises initialize + hover + documentSymbol. It's skipped automatically
// when gopls is not on $PATH so that CI without Go tools keeps passing.
//
// On a warm machine the test completes in ~1s; the long timeout protects
// against first-load penalties after a Go version upgrade.
func TestManagerSmoke(t *testing.T) {
	// silence noisy info logs during the test
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager(logger)
	defer m.Close()

	// Use this test file's module root. runtime.Caller would work but is
	// fragile; the current working directory is the package directory
	// when "go test" runs the test, and its parent is the module root.
	const root = "../../"
	c, err := m.For(ctx, mustAbs(t, root))
	if err != nil {
		t.Skipf("gopls unavailable: %v", err)
	}

	// Hover on the package clause of this file's first line. gopls
	// returns something sensible for the "gopls" identifier (the
	// package path and doc).
	h, err := c.Hover(ctx, mustAbs(t, "client.go"), 13, 8)
	if err != nil {
		t.Fatalf("hover: %v", err)
	}
	if h == nil || h.Contents.Value == "" {
		t.Fatalf("expected non-empty hover content, got %+v", h)
	}

	syms, err := c.DocumentSymbol(ctx, mustAbs(t, "client.go"))
	if err != nil {
		t.Fatalf("documentSymbol: %v", err)
	}
	if len(syms) == 0 {
		t.Fatalf("expected at least one document symbol in client.go")
	}
}

func mustAbs(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("abs %q: %v", rel, err)
	}
	return abs
}
