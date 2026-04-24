package gui

// ctx_panel.go — CTX editor working directly against oos.ctx.
//
// Layout (HSplit):
//   left  ─ list of ctx ids, refreshed from the database on demand
//   right ─ multiline XML editor + save / delete / new toolbar
//
// The file-system import flow was dropped in favour of direct
// database editing: all OOS installs keep their CTX in oos.ctx,
// and asking users to hand-edit files and hit "Import" was a
// relic of the batch-seed workflow that oos-demo already covers
// through its own seed pipeline.

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	oosui "onisin.com/oos-common/ui"
)

// ctxPanelState holds the editor state for the CTX panel.
type ctxPanelState struct {
	conn    *Connection
	ids     []string
	current string // currently selected id, empty when none
	dirty   bool   // editor has unsaved changes
}

// buildCTXPanel builds the CTX editor panel.
func buildCTXPanel(conn *Connection) fyne.CanvasObject {
	state := &ctxPanelState{conn: conn}

	// ── Editor ────────────────────────────────────────────────────────────
	editor := widget.NewMultiLineEntry()
	editor.Wrapping = fyne.TextWrapOff
	editor.SetPlaceHolder("— keine CTX ausgewählt —")
	editor.OnChanged = func(string) { state.dirty = true }

	statusLabel := widget.NewLabel("")

	// ── Left pane: id list ────────────────────────────────────────────────
	idList := widget.NewList(
		func() int { return len(state.ids) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(state.ids) {
				return
			}
			obj.(*widget.Label).SetText(state.ids[id])
		},
	)

	loadInto := func(id string) {
		xml, found, err := state.conn.Importer().LoadCTXRaw(id)
		if err != nil {
			statusLabel.SetText("Fehler: " + err.Error())
			return
		}
		if !found {
			statusLabel.SetText(fmt.Sprintf("oos.ctx[%s] nicht gefunden", id))
			return
		}
		state.current = id
		editor.SetText(xml)
		state.dirty = false
		statusLabel.SetText(fmt.Sprintf("geladen: oos.ctx[%s]", id))
	}

	idList.OnSelected = func(id widget.ListItemID) {
		if id >= len(state.ids) {
			return
		}
		loadInto(state.ids[id])
	}

	refreshList := func() {
		if !conn.IsConnected() {
			state.ids = nil
			idList.Refresh()
			statusLabel.SetText("nicht verbunden")
			return
		}
		ids, err := conn.Importer().GetCTXIDs()
		if err != nil {
			statusLabel.SetText("Liste: " + err.Error())
			return
		}
		state.ids = ids
		idList.Refresh()
		statusLabel.SetText(fmt.Sprintf("%d Einträge", len(ids)))
	}

	// ── Toolbar actions ───────────────────────────────────────────────────
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		refreshList()
	})
	refreshBtn.Importance = widget.LowImportance

	newBtn := widget.NewButtonWithIcon("Neu", theme.ContentAddIcon(), func() {
		if !conn.IsConnected() {
			statusLabel.SetText("nicht verbunden")
			return
		}
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		idEntry := widget.NewEntry()
		idEntry.SetPlaceHolder("z.B. customer")
		dialog.ShowForm("Neue CTX", "Anlegen", "Abbrechen",
			[]*widget.FormItem{{Text: "ID", Widget: idEntry}},
			func(ok bool) {
				if !ok || idEntry.Text == "" {
					return
				}
				id := idEntry.Text
				skeleton := fmt.Sprintf(
					"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<oos>\n  <context name=\"%s\" kind=\"entity\">\n    <!-- fields, relations, permissions here -->\n  </context>\n</oos>\n",
					id,
				)
				if err := conn.Importer().ImportCTXFile(id, skeleton); err != nil {
					statusLabel.SetText("Anlegen: " + err.Error())
					return
				}
				refreshList()
				loadInto(id)
			}, w)
	})

	saveBtn := widget.NewButtonWithIcon("Speichern", theme.DocumentSaveIcon(), func() {
		if state.current == "" {
			statusLabel.SetText("nichts geladen")
			return
		}
		if err := conn.Importer().ImportCTXFile(state.current, editor.Text); err != nil {
			statusLabel.SetText("Speichern: " + err.Error())
			return
		}
		state.dirty = false
		statusLabel.SetText(fmt.Sprintf("gespeichert: oos.ctx[%s]", state.current))
	})
	saveBtn.Importance = widget.HighImportance

	deleteBtn := widget.NewButtonWithIcon("Löschen", theme.DeleteIcon(), func() {
		if state.current == "" {
			statusLabel.SetText("nichts geladen")
			return
		}
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		oosui.ShowWarningConfirm(
			"CTX löschen",
			fmt.Sprintf("Eintrag oos.ctx[%s] wirklich löschen?", state.current),
			"Löschen", "Abbrechen",
			func(ok bool) {
				if !ok {
					return
				}
				if err := conn.Importer().DeleteCTX(state.current); err != nil {
					statusLabel.SetText("Löschen: " + err.Error())
					return
				}
				state.current = ""
				editor.SetText("")
				refreshList()
			}, w)
	})

	toolbar := container.NewHBox(newBtn, saveBtn, deleteBtn)

	// ── Layout ────────────────────────────────────────────────────────────
	leftHeader := container.NewBorder(nil, nil, widget.NewLabel("CTX"), refreshBtn)
	leftPanel := container.NewBorder(leftHeader, nil, nil, nil, idList)

	rightPanel := container.NewBorder(toolbar, statusLabel, nil, nil, editor)

	split := container.NewHSplit(leftPanel, rightPanel)
	split.SetOffset(0.25)

	// First load if already connected at build time.
	refreshList()

	return split
}
