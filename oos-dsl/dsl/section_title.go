// section_title.go — Abschnitts-Titel im DaisyUI-Stil.
package dsl

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// sectionTitle rendert Separator + Label (11px, uppercase).
// color bestimmt die Textfarbe des Titels.
func sectionTitle(text string, col color.Color) fyne.CanvasObject {
	lbl := canvas.NewText(strings.ToUpper(text), col)
	lbl.TextSize = 11
	return container.NewVBox(widget.NewSeparator(), lbl)
}
