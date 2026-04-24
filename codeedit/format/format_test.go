package format

import (
	"strings"
	"testing"
)

// TestFormatXMLReindents verifies that a minified XML document comes
// out with sensible indentation and that Processing Instructions plus
// comments survive the round-trip unchanged.
func TestFormatXMLReindents(t *testing.T) {
	in := `<?xml version="1.0"?><root><!-- comment --><child k="v"/></root>`

	out, err := FormatXML(in)
	if err != nil {
		t.Fatalf("FormatXML returned error: %v", err)
	}

	want := []string{
		`<?xml version="1.0"?>`,
		`<!-- comment -->`,
		`<child k="v"/>`,
	}
	for _, s := range want {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\ngot:\n%s", s, out)
		}
	}

	// Crude indent check: the child line should start with two
	// spaces so the tree structure is visible.
	if !strings.Contains(out, "\n  <child") {
		t.Errorf("expected <child> indented by two spaces, got:\n%s", out)
	}
}

// TestFormatXMLRejectsMalformed ensures that malformed input returns
// an error rather than silently producing garbage. Useful for the UI
// button which should keep the unformatted text on failure.
func TestFormatXMLRejectsMalformed(t *testing.T) {
	_, err := FormatXML(`<oops>`)
	if err == nil {
		t.Fatal("expected error for malformed XML, got nil")
	}
}

// TestFormatGoReindents sanity-checks that the Go formatter touches
// whitespace but not semantics.
func TestFormatGoReindents(t *testing.T) {
	in := "package p\nfunc Hello(){x:=1;_=x}\n"

	out, err := FormatGo(in)
	if err != nil {
		t.Fatalf("FormatGo returned error: %v", err)
	}
	if !strings.Contains(out, "func Hello()") {
		t.Errorf("output missing formatted function; got:\n%s", out)
	}
	if strings.Contains(out, "{x:=1;") {
		t.Errorf("expected gofmt to break the one-line body; got:\n%s", out)
	}
}
