package ui

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	base "onisin.com/oos-dsl-base/base"
	"onisin.com/oos-builder/builder"
	"onisin.com/oos-builder/schema"
)

// NewPropertiesPanel builds the editor that rerenders for the
// currently selected node.
//
// One field per attribute the grammar permits on this element type.
// Required attributes are marked with a trailing asterisk; absent
// values for them stay editable but the field shows the placeholder.
//
// Implementation note: the panel rebuilds its form from scratch on
// every selection change. That sidesteps the awkward state matching
// you'd otherwise need ("did the user just select a different
// element with overlapping attribute names?"). Forms with fewer than
// twenty fields rebuild in microseconds — no perceivable cost.
func NewPropertiesPanel(tree *builder.Tree, cat *schema.Catalog) fyne.CanvasObject {
	header := widget.NewLabelWithStyle("Properties", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	body := container.NewVBox()
	scroll := container.NewVScroll(body)

	rebuild := func() {
		body.RemoveAll()
		sel := tree.Selected
		if sel == nil {
			body.Add(widget.NewLabel("Kein Element ausgewählt."))
			body.Refresh()
			return
		}
		typeLabel := widget.NewLabelWithStyle(
			string(sel.Type),
			fyne.TextAlignLeading,
			fyne.TextStyle{Italic: true},
		)
		body.Add(typeLabel)

		el := cat.Get(string(sel.Type))
		if el == nil {
			body.Add(widget.NewLabel("Element nicht in der Grammatik — keine Attribute bekannt."))
			body.Refresh()
			return
		}
		if len(el.Attrs) == 0 {
			body.Add(widget.NewLabel("Dieses Element hat keine Attribute."))
			body.Refresh()
			return
		}

		// The selection may change before a field's callback runs
		// (very fast clicking through the tree). Capture the current
		// node so callbacks always edit the node they were built for.
		current := sel
		for _, a := range el.Attrs {
			body.Add(buildAttrEditor(tree, current, a))
		}
		body.Refresh()
	}

	rebuild()
	tree.OnChange(func() {
		fyne.Do(rebuild)
	})

	return container.NewBorder(header, nil, nil, nil, scroll)
}

// buildAttrEditor returns one labelled row for a single attribute.
// The editor type is picked from a.Kind: a select for enums, a check
// for booleans, an entry for everything else (with light coercion for
// ints).
func buildAttrEditor(tree *builder.Tree, node *base.Node, a schema.Attr) fyne.CanvasObject {
	label := a.Name
	if a.Required {
		label += " *"
	}
	header := widget.NewLabel(label)

	current := node.Attrs[a.Name]

	var editor fyne.CanvasObject
	switch a.Kind {
	case schema.AttrBool:
		check := widget.NewCheck("", func(checked bool) {
			val := ""
			if checked {
				val = "true"
			}
			tree.SetAttr(node, a.Name, val)
		})
		check.Checked = current == "true" || current == "1"
		editor = check

	case schema.AttrEnum:
		options := append([]string{""}, a.Enums...)
		sel := widget.NewSelect(options, func(v string) {
			tree.SetAttr(node, a.Name, v)
		})
		sel.SetSelected(current)
		editor = sel

	case schema.AttrInt:
		entry := widget.NewEntry()
		entry.SetText(current)
		entry.OnChanged = func(v string) {
			if v == "" {
				tree.SetAttr(node, a.Name, "")
				return
			}
			if _, err := strconv.Atoi(v); err != nil {
				// Don't write garbage; let the user keep typing.
				return
			}
			tree.SetAttr(node, a.Name, v)
		}
		editor = entry

	default:
		entry := widget.NewEntry()
		entry.SetText(current)
		entry.OnChanged = func(v string) {
			tree.SetAttr(node, a.Name, v)
		}
		editor = entry
	}

	return container.NewBorder(nil, nil, header, nil, editor)
}
