package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	base "onisin.com/oos-dsl-base/base"
	"onisin.com/oos-builder/builder"
)

// NewTreePanel builds the hierarchy view on the right.
//
// Day 1 implementation: a flat List of all tree nodes in depth-first
// order, with two-space indent per level. Click selects, the canvas
// and properties panel react via OnChange.
//
// Reason for the flat list (and not Fyne's widget.Tree): widget.Tree
// keeps its own internal model keyed by string IDs, which means we'd
// have to maintain a parallel id <-> *base.Node map and refresh it
// on every mutation. The flat list collapses straight from a
// depth-first walk and refreshes in one call.
//
// A "Remove" button at the bottom deletes the current selection. It
// is intentionally low-friction — undo will land later.
func NewTreePanel(tree *builder.Tree) fyne.CanvasObject {
	var rows []treeRow
	rebuild := func() {
		rows = rows[:0]
		walk(tree.Root, 0, &rows)
	}
	rebuild()

	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(rows) {
				return
			}
			r := rows[id]
			lbl := obj.(*widget.Label)
			lbl.SetText(r.display())
			if r.node == tree.Selected {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				lbl.TextStyle = fyne.TextStyle{}
			}
			lbl.Refresh()
		},
	)

	list.OnSelected = func(id widget.ListItemID) {
		if id >= len(rows) {
			return
		}
		tree.Select(rows[id].node)
	}

	removeBtn := widget.NewButtonWithIcon("Entfernen", theme.DeleteIcon(), func() {
		if tree.Selected == nil || tree.Selected == tree.Root {
			return
		}
		tree.Remove(tree.Selected)
	})
	removeBtn.Importance = widget.LowImportance

	tree.OnChange(func() {
		fyne.Do(func() {
			rebuild()
			list.Refresh()
			// Mirror the model selection into the widget so the
			// highlight follows programmatic changes (palette
			// inserts, removals, etc.).
			for i, r := range rows {
				if r.node == tree.Selected {
					list.Select(i)
					return
				}
			}
			list.UnselectAll()
		})
	})

	return container.NewBorder(
		widget.NewLabelWithStyle("Tree", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		removeBtn,
		nil, nil,
		list,
	)
}

// treeRow is one entry in the flat depth-first projection of the tree.
type treeRow struct {
	node  *base.Node
	depth int
}

// display builds the per-row label. The rule: indent + tag + a hint
// drawn from the most identifying attribute (id > label > bind).
// Without a hint the label collapses to just the tag — which is fine,
// but most realistic screens have labels everywhere.
func (r treeRow) display() string {
	var b strings.Builder
	for i := 0; i < r.depth; i++ {
		b.WriteString("  ")
	}
	b.WriteString(string(r.node.Type))
	if hint := identifyingHint(r.node); hint != "" {
		fmt.Fprintf(&b, "  [%s]", hint)
	}
	return b.String()
}

// identifyingHint picks the first non-empty value from a small list
// of attributes that humans use to recognise a node.
func identifyingHint(n *base.Node) string {
	for _, key := range []string{"id", "label", "title", "bind"} {
		if v := n.Attrs[key]; v != "" {
			return v
		}
	}
	return ""
}

// walk performs a depth-first traversal collecting (node, depth) pairs.
func walk(n *base.Node, depth int, out *[]treeRow) {
	if n == nil {
		return
	}
	*out = append(*out, treeRow{node: n, depth: depth})
	for _, c := range n.Children {
		walk(c, depth+1, out)
	}
}
