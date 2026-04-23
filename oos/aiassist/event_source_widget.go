package aiassist

// event_source_widget.go — Clickable source row with right-click context menu.
//
// Shows a single RAG hit as a clickable row. Right-click opens a small menu
// with "Show event" which spawns a detail window containing the full event
// data as formatted JSON.
//
// Fyne has no native context menu on arbitrary widgets so we implement
// desktop.Mouseable on a custom widget.

import (
	"encoding/json"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// eventSourceWidget is a clickable source row with a right-click menu.
type eventSourceWidget struct {
	widget.BaseWidget

	app   fyne.App
	hit   EventHit
	label string
	text  *canvas.Text
}

// newEventSourceWidget creates a new row for a single event hit.
// nr is the 1-based hit number shown in the label.
func newEventSourceWidget(app fyne.App, nr int, hit EventHit) *eventSourceWidget {
	w := &eventSourceWidget{
		app:   app,
		hit:   hit,
		label: fmt.Sprintf("  [%d] %s | stream: %s | score: %.2f", nr, hit.EventType, hit.StreamID, hit.Score),
	}
	w.ExtendBaseWidget(w)
	return w
}

// CreateRenderer implements fyne.Widget.
func (w *eventSourceWidget) CreateRenderer() fyne.WidgetRenderer {
	w.text = canvas.NewText(w.label, theme.ForegroundColor())
	w.text.TextSize = 13
	return widget.NewSimpleRenderer(w.text)
}

// MouseDown implements desktop.Mouseable — right click opens the context menu.
func (w *eventSourceWidget) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonSecondary {
		return
	}
	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Show event", func() {
			openEventDetailWindow(w.app, w.hit)
		}),
	)
	c := fyne.CurrentApp().Driver().CanvasForObject(w)
	if c != nil {
		widget.ShowPopUpMenuAtPosition(menu, c, ev.AbsolutePosition)
	}
}

// MouseUp implements desktop.Mouseable.
func (w *eventSourceWidget) MouseUp(_ *desktop.MouseEvent) {}

// openEventDetailWindow spawns a secondary window with the formatted event JSON.
func openEventDetailWindow(app fyne.App, hit EventHit) {
	win := app.NewWindow(fmt.Sprintf("Event — %s", hit.EventType))
	win.Resize(fyne.NewSize(600, 400))

	data := map[string]any{
		"source_id":    hit.SourceID,
		"stream_id":    hit.StreamID,
		"event_type":   hit.EventType,
		"mapping":      hit.MappingName,
		"score":        hit.Score,
		"text_content": hit.TextContent,
		"metadata":     hit.Metadata,
	}
	formatted, err := json.MarshalIndent(data, "", "  ")
	jsonText := string(formatted)
	if err != nil {
		jsonText = fmt.Sprintf("error: %v", err)
	}

	entry := widget.NewMultiLineEntry()
	entry.SetText(jsonText)
	entry.Wrapping = fyne.TextWrapOff

	closeBtn := widget.NewButton("Close", win.Close)

	content := container.NewBorder(
		widget.NewLabel(fmt.Sprintf("%s  |  %s", hit.EventType, hit.StreamID)),
		closeBtn,
		nil, nil,
		container.NewScroll(entry),
	)

	win.SetContent(content)
	win.Show()
}
