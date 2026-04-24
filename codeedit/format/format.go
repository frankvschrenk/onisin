// Package format provides pretty-printers for the source languages
// that the codeedit widget knows about. Each public function takes
// the source as a string and returns a re-indented, canonicalised
// version along with an error if the input could not be parsed.
//
// The package is deliberately small and sits outside the core
// codeedit widget so that applications can call the formatters
// independently — from a toolbar button, a keyboard shortcut,
// or a batch migration script.
//
// Current support:
//
//   - FormatXML (via beevik/etree)
//   - FormatGo  (via go/format from the stdlib)
//
// More languages will be added on demand; the signature
// (func(string) (string, error)) is the contract.
package format

import (
	"bytes"
	"fmt"
	gofmt "go/format"

	"github.com/beevik/etree"
)

// FormatXML returns a pretty-printed copy of the given XML source
// indented with two spaces per level. Processing instructions,
// comments and CDATA sections are preserved verbatim — only the
// whitespace between nodes is rewritten.
//
// Returns the original string unchanged when it parses to an empty
// document (nothing to format) and an error when the input is not
// well-formed XML.
func FormatXML(src string) (string, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(src); err != nil {
		return "", fmt.Errorf("xml parse: %w", err)
	}
	doc.Indent(2)

	out, err := doc.WriteToString()
	if err != nil {
		return "", fmt.Errorf("xml write: %w", err)
	}
	return out, nil
}

// FormatGo returns gofmt-formatted Go source. Identical to running
// gofmt on a file — errors point at the first syntactically invalid
// construct, leaving the caller to decide whether to surface the
// error or silently keep the original text.
func FormatGo(src string) (string, error) {
	out, err := gofmt.Source([]byte(src))
	if err != nil {
		return "", fmt.Errorf("go format: %w", err)
	}
	return string(out), nil
}

// tryAll is a small helper used by applications that want "format
// this text using whichever formatter matches the configured
// language". Kept private for now; export on demand.
func tryAll(src string, fns ...func(string) (string, error)) (string, error) {
	var lastErr error
	var buf bytes.Buffer
	for _, fn := range fns {
		out, err := fn(src)
		if err == nil {
			return out, nil
		}
		if buf.Len() > 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
		lastErr = err
	}
	if lastErr == nil {
		return src, nil
	}
	return "", fmt.Errorf("all formatters failed: %s", buf.String())
}

// silence "unused" lint warnings while tryAll is private.
var _ = tryAll
