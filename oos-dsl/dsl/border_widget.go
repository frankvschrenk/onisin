// border_widget.go — Hilfs-Widget das dem Border-Container eine Mindesthöhe gibt.
// Ohne diese würde <border> in einer VBox auf Höhe 0 kollabieren.
package dsl

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// minHeightSpacer ist ein unsichtbares Widget das nur eine Mindesthöhe liefert.
type minHeightSpacer struct {
	widget.BaseWidget
	h float32
}

func newMinHeightSpacer(h float32) *minHeightSpacer {
	s := &minHeightSpacer{h: h}
	s.ExtendBaseWidget(s)
	return s
}

func (s *minHeightSpacer) MinSize() fyne.Size {
	return fyne.NewSize(0, s.h)
}

func (s *minHeightSpacer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewWithoutLayout())
}
