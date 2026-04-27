package schema

import (
	"os"
	"testing"
)

// TestParseCatalog_RealXSD parses the actual dsl.xsd from the repo
// and asserts a handful of things we know to be true. It serves both
// as a regression net and as a quick way to eyeball the parser's
// behaviour against the live grammar.
func TestParseCatalog_RealXSD(t *testing.T) {
	xsd, err := os.ReadFile("../../dsl.xsd")
	if err != nil {
		t.Fatalf("read dsl.xsd: %v", err)
	}

	cat, err := ParseCatalog(xsd)
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}

	if cat.RootName != "screen" {
		t.Errorf("root = %q, want %q", cat.RootName, "screen")
	}

	// Spot-check a few elements that must be present.
	want := []string{
		"screen", "box", "section", "tabs", "tab", "accordion",
		"accordion-item", "field", "table", "column", "button",
	}
	for _, name := range want {
		if cat.Get(name) == nil {
			t.Errorf("missing element %q", name)
		}
	}

	// section: must carry both label (string) and cols (int) plus
	// inherited spacing attributes.
	section := cat.Get("section")
	if section == nil {
		t.Fatal("section missing")
	}
	mustHaveAttr(t, section, "label", AttrString)
	mustHaveAttr(t, section, "cols", AttrInt)
	mustHaveAttr(t, section, "p", AttrString) // from Spacing group

	// screen.id is required.
	screen := cat.Get("screen")
	if screen == nil {
		t.Fatal("screen missing")
	}
	for _, a := range screen.Attrs {
		if a.Name == "id" && !a.Required {
			t.Errorf("screen.id should be required")
		}
	}

	// label-color is an enum with at least the canonical values.
	for _, a := range section.Attrs {
		if a.Name == "label-color" {
			if a.Kind != AttrEnum {
				t.Errorf("label-color kind = %v, want AttrEnum", a.Kind)
			}
			if len(a.Enums) < 5 {
				t.Errorf("label-color enums = %v, want >= 5 values", a.Enums)
			}
		}
	}

	// accordion accepts only accordion-item children.
	acc := cat.Get("accordion")
	if acc == nil {
		t.Fatal("accordion missing")
	}
	if len(acc.Children) != 1 || acc.Children[0] != "accordion-item" {
		t.Errorf("accordion children = %v, want [accordion-item]", acc.Children)
	}

	// section pulls in the UIElement choice — it must accept many
	// children including box, field and table.
	for _, want := range []string{"box", "field", "table"} {
		if !contains(section.Children, want) {
			t.Errorf("section children missing %q (got %v)", want, section.Children)
		}
	}

	// Categories: section is a container, field is a form field.
	if section.Category != CategoryContainer {
		t.Errorf("section category = %v, want Container", section.Category)
	}
	if cat.Get("field").Category != CategoryFormField {
		t.Errorf("field category wrong")
	}
}

func mustHaveAttr(t *testing.T, el *Element, name string, kind AttrKind) {
	t.Helper()
	for _, a := range el.Attrs {
		if a.Name == name {
			if a.Kind != kind {
				t.Errorf("%s.%s kind = %v, want %v", el.Name, name, a.Kind, kind)
			}
			return
		}
	}
	t.Errorf("%s missing attribute %q (got %v)", el.Name, name, attrNames(el))
}

func attrNames(el *Element) []string {
	names := make([]string, len(el.Attrs))
	for i, a := range el.Attrs {
		names[i] = a.Name
	}
	return names
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
