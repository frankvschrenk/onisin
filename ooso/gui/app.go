package gui

// app.go — ooso GUI-Einstiegspunkt.

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Run startet die ooso GUI. Blockiert bis das Fenster geschlossen wird.
func Run() {
	a := app.NewWithID("ai.onisin.ooso")
	w := a.NewWindow("ooso — OOS Synthetist")
	w.Resize(fyne.NewSize(1100, 720))
	w.SetMaster()

	conn := newConnection()

	// Platzhalter — Panels werden erst nach ShowAndRun gebaut
	// damit fyne.CurrentApp() verfügbar ist
	content := container.NewStack(widget.NewLabel(""))
	topBar := buildConnectBar(conn)
	w.SetContent(container.NewBorder(topBar, nil, nil, nil, content))

	// Panels lazy bauen — erst wenn Fyne-Event-Loop läuft
	w.Canvas().SetOnTypedKey(nil) // Trigger für ersten Render
	go func() {
		fyne.Do(func() {
			themePanel, openPreview := buildThemePanel(conn)

			panels := map[string]fyne.CanvasObject{
				"ctx":   buildCTXPanel(conn),
				"dsl":   buildDSLPanel(conn),
				"theme": themePanel,
			}

			content.Objects = []fyne.CanvasObject{panels["ctx"]}
			content.Refresh()

			switchTo := func(name string) {
				p, ok := panels[name]
				if !ok {
					return
				}
				content.Objects = []fyne.CanvasObject{p}
				content.Refresh()
				if name == "theme" {
					openPreview()
				}
			}

			sidebar := buildSidebar(switchTo)
			split := container.NewHSplit(sidebar, content)
			split.SetOffset(0.18)

			w.SetContent(container.NewBorder(topBar, nil, nil, nil, split))
		})
	}()

	w.ShowAndRun()
}
