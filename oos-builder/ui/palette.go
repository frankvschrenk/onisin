// Package ui contains the Fyne widgets that make up the visual DSL
// builder. Each panel is a free-standing constructor that takes the
// shared *builder.Tree plus whatever schema/catalog it needs and
// returns a fyne.CanvasObject ready to embed in the window layout.
//
// The panels do NOT communicate with each other directly. They all
// register Tree.OnChange listeners and rerender from the tree on
// every mutation. That keeps the wiring trivial — adding a new panel
// later is a one-liner in window.go and an OnChange in the new file.
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	base "onisin.com/oos-dsl-base/base"
	"onisin.com/oos-builder/builder"
	"onisin.com/oos-builder/schema"
)

// NewPalette builds the left-hand element palette.
//
// Behaviour: clicking an element inserts a fresh node of that type
// as the last child of the current selection, if the selection is a
// container that may legally contain it. If the selection is a leaf
// (or wouldn't accept the element), the click is ignored and a small
// status hint at the bottom explains why.
//
// Drag-and-drop is not implemented yet — that is Tag 2 work. Click-
// to-insert covers most of what users want and proves the round trip.
func NewPalette(tree *builder.Tree, cat *schema.Catalog) fyne.CanvasObject {
	hint := widget.NewLabel("")
	hint.Wrapping = fyne.TextWrapWord

	groups := cat.ByCategory()

	// One scrollable VBox holding header + per-category buckets.
	col := container.NewVBox()
	for _, g := range groups {
		// Section header.
		header := widget.NewLabelWithStyle(
			g.Category.String(),
			fyne.TextAlignLeading,
			fyne.TextStyle{Bold: true},
		)
		col.Add(header)

		// One button per element in the group. The button looks up
		// the element fresh on every click rather than capturing it
		// here, so the only purpose of the lookup at build time is
		// to skip names that have no entry in the catalog at all
		// (defensive — should not happen with the real grammar).
		for _, name := range g.Names {
			// "screen" is the document root — never insertable.
			if name == "screen" {
				continue
			}
			if cat.Get(name) == nil {
				continue
			}
			btn := newPaletteButton(name, tree, cat, hint)
			col.Add(btn)
		}

		col.Add(widget.NewSeparator())
	}

	scroll := container.NewVScroll(col)
	return container.NewBorder(
		widget.NewLabelWithStyle("Palette", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		hint,  // bottom: status / explanation
		nil, nil,
		scroll,
	)
}

// newPaletteButton wires a single element button. The button stays
// enabled regardless of selection state — disabling buttons on every
// selection change is annoying UX (the user clicks, nothing happens,
// they don't know why). Instead the click checks whether the
// insertion would be legal and writes to the hint label otherwise.
func newPaletteButton(
	name string,
	tree *builder.Tree,
	cat *schema.Catalog,
	hint *widget.Label,
) fyne.CanvasObject {
	btn := widget.NewButton(name, func() {
		sel := tree.Selected
		if sel == nil {
			hint.SetText("Erst einen Container im Tree auswählen.")
			return
		}
		parentEl := cat.Get(string(sel.Type))
		if parentEl == nil {
			hint.SetText("Auswahl unbekannt: " + string(sel.Type))
			return
		}
		if !accepts(parentEl, name) {
			hint.SetText(string(sel.Type) + " kann kein " + name + " enthalten.")
			return
		}
		newNode := makeSkeleton(name, cat.Get(name))
		tree.AppendChild(sel, newNode)
		hint.SetText(name + " eingefügt.")
	})
	btn.Importance = widget.LowImportance
	btn.Alignment = widget.ButtonAlignLeading
	btn.IconPlacement = widget.ButtonIconLeadingText
	btn.SetIcon(theme.ContentAddIcon())
	return btn
}

// accepts reports whether parentEl's grammar permits a child element
// of the given name.
func accepts(parentEl *schema.Element, child string) bool {
	for _, c := range parentEl.Children {
		if c == child {
			return true
		}
	}
	return false
}

// makeSkeleton constructs a fresh node with sensible default
// attributes so the user sees something useful immediately. Required
// attributes (Required==true) get placeholder values; the user can
// edit them in the properties panel right after insertion.
func makeSkeleton(name string, el *schema.Element) *base.Node {
	n := &base.Node{
		Type:  base.NodeType(name),
		Attrs: map[string]string{},
	}
	if el == nil {
		return n
	}
	for _, a := range el.Attrs {
		if !a.Required {
			continue
		}
		switch a.Kind {
		case schema.AttrEnum:
			if len(a.Enums) > 0 {
				n.Attrs[a.Name] = a.Enums[0]
			}
		case schema.AttrInt:
			n.Attrs[a.Name] = "1"
		case schema.AttrBool:
			n.Attrs[a.Name] = "false"
		default:
			n.Attrs[a.Name] = placeholder(name, a.Name)
		}
	}
	return n
}

// placeholder returns a friendlier default than just the attribute
// name. Most required attributes are labels or ids that the user
// will overwrite immediately, but giving them something descriptive
// makes the freshly inserted element legible in the canvas right
// away.
func placeholder(elementName, attrName string) string {
	switch attrName {
	case "id":
		return elementName
	case "label":
		return "neu"
	}
	return ""
}
