package store

// dsl_chunk_test.go — smoke tests for the DSL chunk builders.
//
// Verifies that the element chunker produces a sensible set of chunks
// from the real dsl.xsd at /Users/frank/repro/onisin/dsl.xsd, and that
// the pattern chunker renders a structurally complete pattern chunk
// for a typical screen.
//
// The tests are tolerant: exact output is expected to evolve as the
// chunk format is tuned. They only assert the shape (chunk count,
// known IDs, presence of key sections) so they catch regressions in
// the parser without locking the rendering in place.

import (
	"os"
	"strings"
	"testing"
)

// xsdPath is the absolute path to the authoritative grammar. Keeping
// it as a package-level const makes the test read naturally and makes
// the dependency obvious — this test is not hermetic on purpose.
const xsdPath = "/Users/frank/repro/onisin/dsl.xsd"

func TestBuildDSLElementChunks_FromRealXSD(t *testing.T) {
	data, err := os.ReadFile(xsdPath)
	if err != nil {
		t.Skipf("dsl.xsd not found at %s — skipping (not running under the repo)", xsdPath)
	}

	chunks, err := BuildDSLElementChunks(string(data))
	if err != nil {
		t.Fatalf("BuildDSLElementChunks: %v", err)
	}

	if len(chunks) < 20 {
		t.Errorf("want at least 20 element chunks, got %d", len(chunks))
	}

	wantIDs := []string{
		"element:screen",
		"element:section",
		"element:field",
		"element:tabs",
		"element:tab",
		"element:accordion",
		"element:accordion-item",
		"element:table",
		"element:column",
		"element:button",
		"element:toolbar",
		"element:richtext",
		"element:span",
	}
	got := make(map[string]DSLChunk, len(chunks))
	for _, c := range chunks {
		got[c.ID] = c
	}
	for _, id := range wantIDs {
		if _, ok := got[id]; !ok {
			t.Errorf("missing chunk id %q", id)
		}
	}

	// Every chunk must declare its kind correctly and carry a Purpose
	// line — that's the minimum contract callers rely on.
	for _, c := range chunks {
		if c.Kind != "element" {
			t.Errorf("chunk %s: kind = %q, want 'element'", c.ID, c.Kind)
		}
		if !strings.Contains(c.Text, "Purpose:") {
			t.Errorf("chunk %s missing Purpose line: %s", c.ID, c.Text)
		}
	}

	// Spot-check the <field> chunk — it's the most important one for
	// the LLM and exercises all rendering features (attributes, enum
	// type hint, children, example).
	field := got["element:field"]
	for _, needle := range []string{"<field>", "widget", "bind", "Example:"} {
		if !strings.Contains(field.Text, needle) {
			t.Errorf("field chunk missing %q, got:\n%s", needle, field.Text)
		}
	}
	// widget= is an enum — the chunk should list the values.
	if !strings.Contains(field.Text, "entry") || !strings.Contains(field.Text, "textarea") {
		t.Errorf("field chunk missing widget enum values; got:\n%s", field.Text)
	}
}

func TestBuildDSLPatternChunk_Seed(t *testing.T) {
	const sample = `<screen id="person_detail" title="Person — Detail" label-color="primary"
        delete="true" save="true" exit="true">
  <toolbar>
    <button action="on_edit" style="edit" label="Edit"/>
  </toolbar>
  <section label="Personal" p="3">
    <field label="ID" bind="person.id" readonly="true"/>
    <field label="Role" bind="person.role" widget="choices" options="roles"/>
    <check bind="person.active" label="Active"/>
  </section>
</screen>`

	chunk, err := BuildDSLPatternChunk("person_detail", sample)
	if err != nil {
		t.Fatalf("BuildDSLPatternChunk: %v", err)
	}
	if chunk == nil {
		t.Fatal("BuildDSLPatternChunk returned nil chunk")
	}
	if chunk.ID != "pattern:person_detail" {
		t.Errorf("chunk.ID = %q, want 'pattern:person_detail'", chunk.ID)
	}
	if chunk.Kind != "pattern" {
		t.Errorf("chunk.Kind = %q, want 'pattern'", chunk.Kind)
	}

	// The rendered chunk must surface the attributes the retriever
	// cares about: title, flags, bindings with widget hints, and
	// actions keyed by event name.
	for _, needle := range []string{
		"Screen: person_detail",
		"Title: Person — Detail",
		"save", "delete", "exit",
		"Structure: toolbar",
		"Bindings:",
		"person.id",
		"person.role",
		"widget=choices",
		"person.active",
		"Actions:",
		"on_edit",
	} {
		if !strings.Contains(chunk.Text, needle) {
			t.Errorf("pattern chunk missing %q; got:\n%s", needle, chunk.Text)
		}
	}
}

func TestBuildDSLPatternChunk_Malformed(t *testing.T) {
	_, err := BuildDSLPatternChunk("broken", `<not-a-screen/>`)
	if err == nil {
		t.Fatal("want error for non-screen root, got nil")
	}
}
