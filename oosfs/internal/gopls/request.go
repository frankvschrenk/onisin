// Request wrappers: the methods tool handlers actually call.
//
// Each wrapper takes a context and a position (for pointer-style
// requests), opens the document if needed, issues the LSP call, and
// returns a trimmed result shape. Errors from the underlying call are
// returned verbatim so the MCP layer can surface them.

package gopls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Hover calls textDocument/hover at the given zero-based position.
// Returns nil if gopls has nothing to say for that position (a legal
// outcome — e.g. pointing at whitespace).
func (c *Client) Hover(ctx context.Context, absPath string, line, character int) (*Hover, error) {
	if err := c.EnsureOpen(ctx, absPath); err != nil {
		return nil, err
	}
	params := textDocumentPositionParams(absPath, line, character)
	raw, err := c.call(ctx, "textDocument/hover", params)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// The Contents field is a union: MarkupContent{kind,value},
	// MarkedString (string | {language,value}), or an array of either.
	// We decode the envelope first, then normalize Contents by hand.
	var envelope struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range,omitempty"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode hover: %w", err)
	}
	hc, err := normalizeHoverContents(envelope.Contents)
	if err != nil {
		return nil, err
	}
	return &Hover{Contents: hc, Range: envelope.Range}, nil
}

// Definition calls textDocument/definition. Returns every location
// gopls reports; usually one, occasionally zero (unresolved reference)
// or more (interface method with multiple implementations via gopls's
// behavior on interface definitions — rare in practice).
func (c *Client) Definition(ctx context.Context, absPath string, line, character int) ([]Location, error) {
	if err := c.EnsureOpen(ctx, absPath); err != nil {
		return nil, err
	}
	params := textDocumentPositionParams(absPath, line, character)
	raw, err := c.call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

// References calls textDocument/references. The includeDeclaration
// flag controls whether the symbol's own definition appears in the
// result; callers usually want it, so we default to true.
func (c *Client) References(ctx context.Context, absPath string, line, character int, includeDeclaration bool) ([]Location, error) {
	if err := c.EnsureOpen(ctx, absPath); err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(absPath)},
		"position":     map[string]any{"line": line, "character": character},
		"context":      map[string]any{"includeDeclaration": includeDeclaration},
	}
	raw, err := c.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

// DocumentSymbol calls textDocument/documentSymbol and returns the
// hierarchical symbol tree. gopls always returns the hierarchical
// variant when the client advertises support (we do), so a flat
// SymbolInformation decode path is not needed.
func (c *Client) DocumentSymbol(ctx context.Context, absPath string) ([]DocumentSymbol, error) {
	if err := c.EnsureOpen(ctx, absPath); err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(absPath)},
	}
	raw, err := c.call(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var syms []DocumentSymbol
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil, fmt.Errorf("decode documentSymbol: %w", err)
	}
	return syms, nil
}

// textDocumentPositionParams builds the common {textDocument, position}
// body used by hover/definition.
func textDocumentPositionParams(absPath string, line, character int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(absPath)},
		"position":     map[string]any{"line": line, "character": character},
	}
}

// decodeLocations accepts the three shapes a "Location-or-LocationLink"
// response can take (null, single Location, array of Location) and
// returns a slice.
func decodeLocations(raw json.RawMessage) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// Try array first.
	var arr []Location
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	// Then single object.
	var single Location
	if err := json.Unmarshal(raw, &single); err == nil {
		return []Location{single}, nil
	}
	return nil, errors.New("unexpected location payload shape")
}

// normalizeHoverContents squashes the Contents union into a single
// {kind, value} pair. We accept:
//   - {kind, value}         — MarkupContent (the common case for gopls)
//   - string                — legacy MarkedString, plain text
//   - {language, value}     — legacy MarkedString, code block
//   - [ ...any of above ]   — legacy array, concatenated with blank lines
func normalizeHoverContents(raw json.RawMessage) (HoverContents, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return HoverContents{}, nil
	}
	// Object variants.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if v, ok := obj["value"].(string); ok {
			kind, _ := obj["kind"].(string)
			if kind == "" {
				// MarkedString with language field — render as a
				// fenced block so the caller sees the language tag.
				if lang, ok := obj["language"].(string); ok {
					return HoverContents{
						Kind:  "markdown",
						Value: "```" + lang + "\n" + v + "\n```",
					}, nil
				}
			}
			return HoverContents{Kind: kind, Value: v}, nil
		}
	}
	// Plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return HoverContents{Kind: "plaintext", Value: s}, nil
	}
	// Array of variants.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, item := range arr {
			sub, err := normalizeHoverContents(item)
			if err != nil {
				continue
			}
			if sub.Value != "" {
				parts = append(parts, sub.Value)
			}
		}
		return HoverContents{Kind: "markdown", Value: strings.Join(parts, "\n\n")}, nil
	}
	return HoverContents{}, errors.New("unexpected hover contents shape")
}
