package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-builder/builder"
	"onisin.com/oos-dsl/dsl"
)

// NewCanvas builds the centre canvas — a live preview of the current
// tree, re-rendered on every mutation.
//
// We deliberately reuse oos-dsl/dsl.Builder rather than writing a
// second renderer. The whole point of the builder is "what you see
// is what oos will render at runtime", so a parallel implementation
// would only invite drift.
//
// Performance: full re-render per mutation is fine for screens of
// realistic size (dozens of nodes). If a screen ever pushes 500+
// nodes we can swap in incremental rebuilds, but that's not Tag 1.
func NewCanvas(tree *builder.Tree) fyne.CanvasObject {
	stack := container.NewStack()

	render := func() {
		root := tree.Root
		if root == nil {
			stack.Objects = []fyne.CanvasObject{
				widget.NewLabel("— leerer Baum —"),
			}
			stack.Refresh()
			return
		}
		st := dsl.NewState()
		// onAction is nil in the builder — we don't execute anything,
		// only render. The dsl builder is permissive about that.
		b := dsl.NewBuilder(st, root.Attrs["id"], nil)
		obj := b.Build(root)
		if obj == nil {
			stack.Objects = []fyne.CanvasObject{
				widget.NewLabel("— leerer Screen —"),
			}
			stack.Refresh()
			return
		}
		stack.Objects = []fyne.CanvasObject{
			container.NewVScroll(obj),
		}
		stack.Refresh()
	}

	// Initial paint plus subscribe to future changes.
	render()
	tree.OnChange(func() {
		// Tree mutations may originate on any goroutine; Fyne demands
		// UI work happen on its own thread.
		fyne.Do(render)
	})

	return container.NewBorder(
		widget.NewLabelWithStyle("Canvas (Live-Preview)", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		stack,
	)
}
