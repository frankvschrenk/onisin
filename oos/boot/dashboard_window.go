package boot

import (
	"encoding/json"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos/aiassist"
	"onisin.com/oos/helper"
)

type dashboardTabs struct {
	tabs        *container.AppTabs
	win         fyne.Window
	eventEntry  *widget.Entry
	dataEntry   *widget.Entry
	resultEntry *widget.Entry
}

func openDashboard() {
	win := fyneApp.NewWindow("OOS Dashboard")
	win.Resize(fyne.NewSize(900, 600))

	d := &dashboardTabs{win: win}
	d.build(win)
	win.Show()
}

func (d *dashboardTabs) build(win fyne.Window) {
	d.eventEntry = newReadOnlyEntry("Event JSON erscheint hier...")
	d.dataEntry = newReadOnlyEntry("Stage-Daten erscheinen hier...")
	d.resultEntry = newReadOnlyEntry("Save/Delete Ergebnis erscheint hier...")

	d.tabs = container.NewAppTabs(
		container.NewTabItem("Welcome", d.buildWelcomeTab()),
		container.NewTabItem("Event", container.NewScroll(d.eventEntry)),
		container.NewTabItem("Daten", container.NewScroll(d.dataEntry)),
		container.NewTabItem("Ergebnis", container.NewScroll(d.resultEntry)),
	)

	win.SetContent(d.tabs)

	helper.AddBoardEventHandler("dashboard", d.handleEvent)
	win.SetOnClosed(func() {
		helper.RemoveBoardEventHandler("dashboard")
	})
}

func (d *dashboardTabs) buildWelcomeTab() fyne.CanvasObject {
	username := helper.ActiveUsername()
	if username == "" {
		username = helper.ActiveEmail()
	}

	form := widget.NewForm(
		widget.NewFormItem("Benutzer", widget.NewLabel(username)),
		widget.NewFormItem("Rolle", widget.NewLabel(helper.ActiveRole)),
		widget.NewFormItem("OOSP", widget.NewLabel(helper.Meta.OOSPUrl)),
		widget.NewFormItem("Version", widget.NewLabel(helper.Meta.Version)),
	)

	// AI Assistant — eino-based chat with AST injection
	aiBtn := widget.NewButton("AI Assistant", func() {
		if err := aiassist.OpenWindow(fyneApp); err != nil {
			dialog.ShowError(err, d.win)
		}
	})

	hint := widget.NewLabel("Settings: Menu OOS → Settings")
	hint.Wrapping = fyne.TextWrapWord

	return container.NewVBox(form, widget.NewSeparator(), aiBtn, widget.NewSeparator(), hint)
}

// handleEvent is invoked by helper.FireBoardEvent, which runs on the
// caller's goroutine — not the Fyne main thread. Every widget mutation
// below must therefore be marshalled via fyne.Do. Compute outside the
// closure, touch widgets only inside.
func (d *dashboardTabs) handleEvent(ev helper.BoardEvent) {
	switch ev.Action {
	case "save_result", "delete_result":
		resultText := ev.Result
		if ev.Error != "" {
			resultText = "Fehler:\n" + ev.Error
		}
		dataText := d.formatStageData()

		fyne.Do(func() {
			d.resultEntry.SetText(resultText)
			d.dataEntry.SetText(dataText)
			d.tabs.SelectIndex(3)
		})
	default:
		if len(ev.JSON) > 0 {
			payload := string(ev.JSON)
			fyne.Do(func() {
				d.eventEntry.SetText(payload)
				d.tabs.SelectIndex(1)
			})
		}
	}
}

// formatStageData renders the current stage as pretty JSON. Pure function —
// safe to call off the UI thread; the caller marshals the result back via
// fyne.Do.
func (d *dashboardTabs) formatStageData() string {
	b, _ := json.MarshalIndent(helper.Stage.CurrentData, "", "  ")
	if len(b) == 0 {
		return "Keine Daten in der Stage."
	}
	return string(b)
}

func newReadOnlyEntry(placeholder string) *widget.Entry {
	e := widget.NewMultiLineEntry()
	e.SetPlaceHolder(placeholder)
	e.Wrapping = fyne.TextWrapWord
	return e
}
