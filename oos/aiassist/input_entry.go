package aiassist

// input_entry.go — Multi-line entry with a custom right-click menu.
//
// Fyne's default Entry already offers Cut/Copy/Paste/Select All via its
// built-in context menu. We subclass it to add a "Clear" item so the
// user can empty the question box with a single click instead of selecting
// everything first.
//
// Since overriding the built-in menu replaces it wholesale, we also keep
// the standard items to avoid surprising users who expect them to work.

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// inputEntry is a MultiLineEntry with a right-click context menu that
// includes a Clear action alongside the usual clipboard items.
type inputEntry struct {
	widget.Entry
}

// newInputEntry creates a configured multi-line entry ready for the chat input bar.
func newInputEntry() *inputEntry {
	e := &inputEntry{}
	e.ExtendBaseWidget(e)
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	return e
}

// TappedSecondary handles right-click by showing a menu with a Clear item.
//
// We intentionally don't chain to the base implementation: Fyne doesn't
// expose a way to extend the default menu, so we replicate the most useful
// items here and let Clear be the headline feature.
func (e *inputEntry) TappedSecondary(ev *fyne.PointEvent) {
	clearItem := fyne.NewMenuItem("Clear", func() {
		e.SetText("")
	})

	cutItem := fyne.NewMenuItem("Cut", func() {
		clipboard := fyne.CurrentApp().Driver().AllWindows()[0].Clipboard()
		clipboard.SetContent(e.SelectedText())
		e.TypedShortcut(&fyne.ShortcutCut{Clipboard: clipboard})
	})
	copyItem := fyne.NewMenuItem("Copy", func() {
		clipboard := fyne.CurrentApp().Driver().AllWindows()[0].Clipboard()
		clipboard.SetContent(e.SelectedText())
	})
	pasteItem := fyne.NewMenuItem("Paste", func() {
		clipboard := fyne.CurrentApp().Driver().AllWindows()[0].Clipboard()
		e.TypedShortcut(&fyne.ShortcutPaste{Clipboard: clipboard})
	})
	selectAllItem := fyne.NewMenuItem("Select All", func() {
		e.TypedShortcut(&fyne.ShortcutSelectAll{})
	})

	menu := fyne.NewMenu("",
		clearItem,
		fyne.NewMenuItemSeparator(),
		cutItem,
		copyItem,
		pasteItem,
		fyne.NewMenuItemSeparator(),
		selectAllItem,
	)

	c := fyne.CurrentApp().Driver().CanvasForObject(e)
	if c != nil {
		widget.ShowPopUpMenuAtPosition(menu, c, ev.AbsolutePosition)
	}
}

// MouseDown forwards to desktop.Mouseable so the right-click event fires.
// Without this, TappedSecondary may not be invoked on all platforms.
func (e *inputEntry) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonSecondary {
		e.TappedSecondary(&fyne.PointEvent{
			Position:         ev.Position,
			AbsolutePosition: ev.AbsolutePosition,
		})
		return
	}
	e.Entry.MouseDown(ev)
}

// MouseUp is the other half of desktop.Mouseable. The base Entry handles
// left-click selection; we just delegate.
func (e *inputEntry) MouseUp(ev *desktop.MouseEvent) {
	e.Entry.MouseUp(ev)
}
