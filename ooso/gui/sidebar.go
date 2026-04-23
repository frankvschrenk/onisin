package gui

// sidebar.go — Navigationssidebar mit drei Einträgen.

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// buildSidebar baut die linke Navigationssidebar.
// Icons werden lazy geholt — erst beim Aufruf, nicht beim Package-Init.
func buildSidebar(switchTo func(string)) fyne.CanvasObject {
	type navItem struct {
		label string
		icon  func() fyne.Resource
		key   string
	}

	items := []navItem{
		{"CTX", theme.FolderIcon, "ctx"},
		{"DSL", theme.DocumentIcon, "dsl"},
		{"Theme", theme.ColorPaletteIcon, "theme"},
	}

	var buttons []fyne.CanvasObject
	for _, item := range items {
		key := item.key
		icon := item.icon()
		btn := widget.NewButtonWithIcon(item.label, icon, func() {
			switchTo(key)
		})
		btn.Alignment = widget.ButtonAlignLeading
		buttons = append(buttons, btn)
	}

	return container.NewVBox(buttons...)
}
