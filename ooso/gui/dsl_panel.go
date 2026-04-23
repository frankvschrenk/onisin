package gui

// dsl_panel.go — DSL editor working directly against oos.dsl.
//
// Layout (HSplit):
//   left   ─ list of screen ids + toolbar (new / save / delete / refresh)
//   middle ─ multiline XML editor
//   right  ─ live preview rendered from the editor's current text
//
// The file-system import flow was dropped: DSL screens are edited in
// place in oos.dsl. Preview re-renders on every save, so the round
// trip "edit XML → save → see it" stays inside the panel.

import (
	"bytes"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-dsl/dsl"
)

// dslPanelState holds the editor state for the DSL panel.
type dslPanelState struct {
	conn    *Connection
	ids     []string
	current string
	dirty   bool
}

// buildDSLPanel builds the DSL editor + preview panel.
func buildDSLPanel(conn *Connection) fyne.CanvasObject {
	state := &dslPanelState{conn: conn}

	// ── Editor ────────────────────────────────────────────────────────────
	editor := widget.NewMultiLineEntry()
	editor.Wrapping = fyne.TextWrapOff
	editor.SetPlaceHolder("— keinen Screen ausgewählt —")
	editor.OnChanged = func(string) { state.dirty = true }

	statusLabel := widget.NewLabel("")

	// ── Preview ───────────────────────────────────────────────────────────
	previewContainer := container.NewStack(
		widget.NewLabel("— Preview erscheint nach Speichern —"),
	)

	renderPreview := func(xml string) {
		root, err := dsl.Parse(bytes.NewReader([]byte(xml)))
		if err != nil {
			previewContainer.Objects = []fyne.CanvasObject{
				widget.NewLabel("Parse-Fehler: " + err.Error()),
			}
			previewContainer.Refresh()
			return
		}
		st := dsl.NewState()
		builder := dsl.NewBuilder(st, state.current, nil)
		screen := builder.Build(root)
		previewContainer.Objects = []fyne.CanvasObject{
			container.NewVScroll(screen),
		}
		previewContainer.Refresh()
	}

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
		xml, found, err := state.conn.Importer().LoadDSLRaw(id)
		if err != nil {
			statusLabel.SetText("Fehler: " + err.Error())
			return
		}
		if !found {
			statusLabel.SetText(fmt.Sprintf("oos.dsl[%s] nicht gefunden", id))
			return
		}
		state.current = id
		editor.SetText(xml)
		state.dirty = false
		statusLabel.SetText(fmt.Sprintf("geladen: oos.dsl[%s]", id))
		renderPreview(xml)
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
		ids, err := conn.Importer().GetDSLIDs()
		if err != nil {
			statusLabel.SetText("Liste: " + err.Error())
			return
		}
		state.ids = ids
		idList.Refresh()
		statusLabel.SetText(fmt.Sprintf("%d Screens", len(ids)))
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
		idEntry.SetPlaceHolder("z.B. customer_detail")
		dialog.ShowForm("Neuer DSL-Screen", "Anlegen", "Abbrechen",
			[]*widget.FormItem{{Text: "Screen-ID", Widget: idEntry}},
			func(ok bool) {
				if !ok || idEntry.Text == "" {
					return
				}
				id := idEntry.Text
				skeleton := fmt.Sprintf(
					"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<screen id=\"%s\">\n  <!-- layout, widgets, bindings here -->\n</screen>\n",
					id,
				)
				if err := conn.Importer().ImportDSL(id, skeleton); err != nil {
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
		if err := conn.Importer().ImportDSL(state.current, editor.Text); err != nil {
			statusLabel.SetText("Speichern: " + err.Error())
			return
		}
		state.dirty = false
		statusLabel.SetText(fmt.Sprintf("gespeichert: oos.dsl[%s]", state.current))
		renderPreview(editor.Text)
	})
	saveBtn.Importance = widget.HighImportance

	deleteBtn := widget.NewButtonWithIcon("Löschen", theme.DeleteIcon(), func() {
		if state.current == "" {
			statusLabel.SetText("nichts geladen")
			return
		}
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		dialog.ShowConfirm(
			"DSL löschen",
			fmt.Sprintf("Screen oos.dsl[%s] wirklich löschen?", state.current),
			func(ok bool) {
				if !ok {
					return
				}
				if err := conn.Importer().DeleteDSL(state.current); err != nil {
					statusLabel.SetText("Löschen: " + err.Error())
					return
				}
				state.current = ""
				editor.SetText("")
				previewContainer.Objects = []fyne.CanvasObject{
					widget.NewLabel("— kein Screen ausgewählt —"),
				}
				previewContainer.Refresh()
				refreshList()
			}, w)
	})

	previewBtn := widget.NewButtonWithIcon("Preview", theme.VisibilityIcon(), func() {
		renderPreview(editor.Text)
	})

	toolbar := container.NewHBox(newBtn, saveBtn, deleteBtn, previewBtn)

	// ── Layout ────────────────────────────────────────────────────────────
	leftHeader := container.NewBorder(nil, nil, widget.NewLabel("DSL"), refreshBtn)
	leftPanel := container.NewBorder(leftHeader, nil, nil, nil, idList)

	middlePanel := container.NewBorder(toolbar, statusLabel, nil, nil, editor)

	rightPanel := container.NewBorder(
		widget.NewLabel("Preview"),
		nil, nil, nil,
		previewContainer,
	)

	// Two-level split: list | (editor | preview)
	editPreviewSplit := container.NewHSplit(middlePanel, rightPanel)
	editPreviewSplit.SetOffset(0.5)

	outerSplit := container.NewHSplit(leftPanel, editPreviewSplit)
	outerSplit.SetOffset(0.2)

	refreshList()

	return outerSplit
}
