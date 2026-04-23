package aiassist

// event_panel.go — The event chat view: top answer, bottom sources.
//
// Layout (VSplit, 60/40):
//
//   ┌─────────────────────────────┐
//   │  LLM answer (Markdown)      │
//   ├─────────────────────────────┤
//   │  Sources (right-click:      │
//   │   show event JSON)          │
//   └─────────────────────────────┘
//
// The panel is a pure view — it doesn't know about networking or LLMs.
// All state mutation goes through the three Set* methods below.

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// eventPanel renders the RAG answer above a clickable source list.
type eventPanel struct {
	app fyne.App

	answer  *widget.RichText
	sources *fyne.Container // holds eventSourceWidget rows
	root    fyne.CanvasObject
}

// newEventPanel builds an empty event panel ready to be embedded.
func newEventPanel(app fyne.App) *eventPanel {
	answer := widget.NewRichTextFromMarkdown("_Answer will appear here. Pick a context, optionally a stream ID, then ask a question._")
	answer.Wrapping = fyne.TextWrapWord

	sources := container.NewVBox()

	split := container.NewVSplit(
		container.NewVScroll(answer),
		container.NewVScroll(sources),
	)
	split.Offset = 0.6

	return &eventPanel{
		app:     app,
		answer:  answer,
		sources: sources,
		root:    split,
	}
}

// canvasObject returns the panel's root widget for embedding.
func (p *eventPanel) canvasObject() fyne.CanvasObject {
	return p.root
}

// SetAnswer replaces the Markdown text shown in the upper half.
func (p *eventPanel) SetAnswer(md string) {
	fyne.Do(func() {
		p.answer.ParseMarkdown(md)
	})
}

// SetSources rebuilds the lower source list from scratch.
// An empty slice shows a "no hits" hint instead.
func (p *eventPanel) SetSources(hits []EventHit) {
	fyne.Do(func() {
		p.sources.RemoveAll()
		if len(hits) == 0 {
			p.sources.Add(widget.NewLabel("No matching events."))
			p.sources.Refresh()
			return
		}
		p.sources.Add(widget.NewLabel(fmt.Sprintf(
			"%d relevant events — right-click for details:", len(hits))))
		for i, h := range hits {
			p.sources.Add(newEventSourceWidget(p.app, i+1, h))
		}
		p.sources.Refresh()
	})
}

// Clear resets the panel to its initial empty state.
func (p *eventPanel) Clear() {
	fyne.Do(func() {
		p.answer.ParseMarkdown("")
		p.sources.RemoveAll()
		p.sources.Refresh()
	})
}
