package builder

import (
	"strings"
	"testing"

	base "onisin.com/oos-dsl-base/base"
)

func TestNewEmpty_RootSelected(t *testing.T) {
	tree := NewEmpty("person_form")
	if tree.Root == nil {
		t.Fatal("root nil")
	}
	if tree.Root.Type != base.NodeScreen {
		t.Errorf("root type = %v, want screen", tree.Root.Type)
	}
	if got := tree.Root.Attrs["id"]; got != "person_form" {
		t.Errorf("id = %q, want person_form", got)
	}
	if tree.Selected != tree.Root {
		t.Errorf("selection should default to root")
	}
}

func TestAppendChild_TracksParentAndSelection(t *testing.T) {
	tree := NewEmpty("s")
	section := &base.Node{Type: base.NodeSection}

	if !tree.AppendChild(tree.Root, section) {
		t.Fatal("AppendChild returned false")
	}
	if tree.Parent(section) != tree.Root {
		t.Errorf("parent index broken")
	}
	if tree.Selected != section {
		t.Errorf("new child should be selected")
	}
	if len(tree.Root.Children) != 1 || tree.Root.Children[0] != section {
		t.Errorf("section not appended to children")
	}
}

func TestRemove_ShiftsSelectionToParent(t *testing.T) {
	tree := NewEmpty("s")
	box := &base.Node{Type: base.NodeBox}
	field := &base.Node{Type: base.NodeField}
	tree.AppendChild(tree.Root, box)
	tree.AppendChild(box, field)

	// field is now selected.
	tree.Remove(field)
	if tree.Selected != box {
		t.Errorf("after removing leaf, selection = %v, want box", tree.Selected)
	}
	if len(box.Children) != 0 {
		t.Errorf("field still attached")
	}
	if tree.Parent(field) != nil {
		t.Errorf("removed node still has parent")
	}
}

func TestRemove_RemovingAncestorOfSelection(t *testing.T) {
	tree := NewEmpty("s")
	box := &base.Node{Type: base.NodeBox}
	field := &base.Node{Type: base.NodeField}
	tree.AppendChild(tree.Root, box)
	tree.AppendChild(box, field) // selection = field
	tree.Remove(box)

	if tree.Selected != tree.Root {
		t.Errorf("selection = %v, want root after ancestor removal", tree.Selected)
	}
}

func TestRemove_RootIsNoOp(t *testing.T) {
	tree := NewEmpty("s")
	tree.Remove(tree.Root)
	if tree.Root == nil {
		t.Error("root removed despite no-op contract")
	}
}

func TestSetAttr_EmptyDeletes(t *testing.T) {
	tree := NewEmpty("s")
	tree.SetAttr(tree.Root, "title", "Hello")
	if tree.Root.Attrs["title"] != "Hello" {
		t.Errorf("set failed")
	}
	tree.SetAttr(tree.Root, "title", "")
	if _, ok := tree.Root.Attrs["title"]; ok {
		t.Errorf("empty value should delete")
	}
}

func TestMoveBefore_ReordersSiblings(t *testing.T) {
	tree := NewEmpty("s")
	a := &base.Node{Type: base.NodeField, Attrs: map[string]string{"label": "A"}}
	b := &base.Node{Type: base.NodeField, Attrs: map[string]string{"label": "B"}}
	c := &base.Node{Type: base.NodeField, Attrs: map[string]string{"label": "C"}}
	tree.AppendChild(tree.Root, a)
	tree.AppendChild(tree.Root, b)
	tree.AppendChild(tree.Root, c)

	if !tree.MoveBefore(c, a) {
		t.Fatal("MoveBefore returned false")
	}
	got := []string{
		tree.Root.Children[0].Attrs["label"],
		tree.Root.Children[1].Attrs["label"],
		tree.Root.Children[2].Attrs["label"],
	}
	want := []string{"C", "A", "B"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

func TestOnChange_FiresForMutations(t *testing.T) {
	tree := NewEmpty("s")
	calls := 0
	tree.OnChange(func() { calls++ })

	tree.SetAttr(tree.Root, "title", "X") // 1
	tree.AppendChild(tree.Root, &base.Node{Type: base.NodeBox}) // 2
	tree.Select(tree.Root) // 3 — selection changed back
	if calls != 3 {
		t.Errorf("listener called %d times, want 3", calls)
	}
}

func TestMarshalXML_RoundTrip(t *testing.T) {
	src := `<screen id="person_detail" title="Person">
  <section label="Adresse" cols="2">
    <field label="Straße" bind="person.street"/>
    <field label="PLZ" bind="person.zip"/>
  </section>
</screen>`

	tree, err := LoadXML(src, "")
	if err != nil {
		t.Fatalf("LoadXML: %v", err)
	}

	out, err := tree.MarshalXML()
	if err != nil {
		t.Fatalf("MarshalXML: %v", err)
	}

	// Spot checks — we don't assert byte-exact equivalence because
	// indent and attribute ordering are normalised by design.
	for _, want := range []string{
		`<screen id="person_detail"`,
		`<section`,
		`label="Adresse"`,
		`cols="2"`,
		`<field`,
		`bind="person.street"`,
		`</section>`,
		`</screen>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}

	// Re-parse the output to make sure the renderer accepts it back.
	tree2, err := LoadXML(out, "")
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got := len(tree2.Root.Children); got != 1 {
		t.Errorf("round-trip lost children: %d", got)
	}
}

func TestMarshalXML_AttrOrderIsStable(t *testing.T) {
	tree := NewEmpty("s")
	tree.SetAttr(tree.Root, "title", "T")
	tree.SetAttr(tree.Root, "scroll", "true")
	tree.SetAttr(tree.Root, "label", "L")

	out, _ := tree.MarshalXML()
	// Priority order: id, label, title, bind. Anything else
	// alphabetically. So expected order: id < label < title < scroll.
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx(`id="s"`) < idx(`label="L"`) && idx(`label="L"`) < idx(`title="T"`) && idx(`title="T"`) < idx(`scroll="true"`)) {
		t.Errorf("attribute order unstable, got:\n%s", out)
	}
}
