package chat

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

type QuelleWidget struct {
	widget.BaseWidget

	label   string
	quelle  Quelle
	app     fyne.App

	text    *canvas.Text
}

func NewQuelleWidget(app fyne.App, nr int, q Quelle) *QuelleWidget {
	w := &QuelleWidget{
		app:    app,
		quelle: q,
		label:  fmt.Sprintf("  [%d] %s | Fall: %s | Score: %.2f", nr, q.EventType, q.StreamID, q.Score),
	}
	w.ExtendBaseWidget(w)
	return w
}

func (w *QuelleWidget) CreateRenderer() fyne.WidgetRenderer {
	w.text = canvas.NewText(w.label, theme.ForegroundColor())
	w.text.TextSize = 13
	return widget.NewSimpleRenderer(w.text)
}

func (w *QuelleWidget) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonSecondary {
		menu := fyne.NewMenu("",
			fyne.NewMenuItem("Zeige Event", func() {
				openEventDetailWindow(w.app, w.quelle)
			}),
		)
		canvas := fyne.CurrentApp().Driver().CanvasForObject(w)
		if canvas != nil {
			widget.ShowPopUpMenuAtPosition(menu, canvas, ev.AbsolutePosition)
		}
	}
}

func (w *QuelleWidget) MouseUp(_ *desktop.MouseEvent) {}

func openEventDetailWindow(app fyne.App, q Quelle) {
	win := app.NewWindow(fmt.Sprintf("Event — %s", q.EventType))
	win.Resize(fyne.NewSize(600, 400))

	data := map[string]any{
		"event_id":   q.EventID,
		"stream_id":  q.StreamID,
		"event_type": q.EventType,
		"score":      q.Score,
		"data":       q.Data,
	}
	formatted, err := json.MarshalIndent(data, "", "  ")
	jsonText := string(formatted)
	if err != nil {
		jsonText = fmt.Sprintf("Fehler: %v", err)
	}

	entry := widget.NewMultiLineEntry()
	entry.SetText(jsonText)
	entry.Wrapping = fyne.TextWrapOff

	closeBtn := widget.NewButton("Schliessen", win.Close)

	content := container.NewBorder(
		widget.NewLabel(fmt.Sprintf("%s  |  %s", q.EventType, q.StreamID)),
		closeBtn,
		nil, nil,
		container.NewScroll(entry),
	)

	win.SetContent(content)
	win.Show()
}
