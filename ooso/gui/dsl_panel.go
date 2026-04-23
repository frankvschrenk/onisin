package gui

// dsl_panel.go — Panel zum Importieren und Previewen von DSL-Dateien.
//
// Layout (HSplit):
//   Links:
//     [ Verzeichnis wählen ]  [ Importieren ]
//     [ Dateiliste mit Checkboxen ]
//     [ Log ]
//   Rechts:
//     [ Datei wählen ]  [ Preview-Button ]
//     [ DSL Preview  ]
//
// Der Benutzer wählt ein Verzeichnis mit *.dsl.xml Dateien.
// Einzelne oder alle können importiert werden.
// Per Klick auf eine Datei öffnet sich die Preview rechts.

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"onisin.com/oos-dsl/dsl"
	"onisin.com/oos-common/importer"
)

// dslPanelState hält den Zustand des DSL-Panels.
type dslPanelState struct {
	conn    *Connection
	files   []*importer.DSLFile
	checks  []bool
	dslDir  string
}

// buildDSLPanel baut das DSL-Import + Preview Panel.
func buildDSLPanel(conn *Connection) fyne.CanvasObject {
	state := &dslPanelState{conn: conn}

	// ── Log ───────────────────────────────────────────────────────────────
	logView := widget.NewMultiLineEntry()
	logView.Wrapping = fyne.TextWrapWord
	logView.Disable()

	appendLog := func(msg string) {
		cur := logView.Text
		if cur != "" {
			cur += "\n"
		}
		logView.SetText(cur + msg)
	}

	// ── Preview-Bereich (rechts) ──────────────────────────────────────────
	previewContainer := container.NewStack(
		widget.NewLabel("← Datei auswählen für Preview"),
	)

	showPreview := func(f *importer.DSLFile) {
		state := dsl.NewState()
		builder := dsl.NewBuilder(state, f.ScreenID, nil)
		screen := builder.Build(f.Root)
		previewContainer.Objects = []fyne.CanvasObject{
			container.NewVScroll(screen),
		}
		previewContainer.Refresh()
	}

	// ── Dateiliste (links) ────────────────────────────────────────────────
	fileList := widget.NewList(
		func() int { return len(state.files) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewCheck("", nil),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			check := row.Objects[0].(*widget.Check)
			label := row.Objects[1].(*widget.Label)
			if id >= len(state.files) {
				return
			}
			f := state.files[id]
			label.SetText(fmt.Sprintf("%s  [%s]", f.Filename, f.ScreenID))
			check.SetChecked(state.checks[id])
			check.OnChanged = func(checked bool) {
				state.checks[id] = checked
			}
		},
	)

	fileList.OnSelected = func(id widget.ListItemID) {
		if id < len(state.files) {
			showPreview(state.files[id])
		}
	}

	// ── Verzeichnis wählen ────────────────────────────────────────────────
	dirLabel := widget.NewLabel("kein Verzeichnis gewählt")

	chooseBtn := widget.NewButtonWithIcon("Verzeichnis wählen…", theme.FolderOpenIcon(), func() {
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err != nil || u == nil {
				return
			}
			dir := u.Path()
			files, err := importer.ParseDSLDir(dir)
			if err != nil {
				appendLog("✗ " + err.Error())
				return
			}
			state.dslDir = dir
			state.files = files
			state.checks = make([]bool, len(files))
			for i := range state.checks {
				state.checks[i] = true
			}
			dirLabel.SetText(dir)
			fileList.Refresh()
			appendLog(fmt.Sprintf("✓ %d DSL-Dateien geladen", len(files)))
		}, w)
	})

	// ── Einzelne Datei wählen ─────────────────────────────────────────────
	addFileBtn := widget.NewButtonWithIcon("Datei hinzufügen…", theme.FileIcon(), func() {
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		dialog.ShowFileOpen(func(f fyne.URIReadCloser, err error) {
			if err != nil || f == nil {
				return
			}
			defer f.Close()
			parsed, err := importer.ParseDSLFile(f.URI().Path())
			if err != nil {
				appendLog("✗ " + err.Error())
				return
			}
			state.files = append(state.files, parsed)
			state.checks = append(state.checks, true)
			fileList.Refresh()
			appendLog(fmt.Sprintf("✓ %s geladen [%s]", parsed.Filename, parsed.ScreenID))
		}, w)
	})

	// ── Alle / Keine ──────────────────────────────────────────────────────
	selectAllBtn := widget.NewButtonWithIcon("Alle", theme.CheckButtonCheckedIcon(), func() {
		for i := range state.checks {
			state.checks[i] = true
		}
		fileList.Refresh()
	})

	selectNoneBtn := widget.NewButtonWithIcon("Keine", theme.CheckButtonIcon(), func() {
		for i := range state.checks {
			state.checks[i] = false
		}
		fileList.Refresh()
	})

	// ── Import ────────────────────────────────────────────────────────────
	importBtn := widget.NewButtonWithIcon("Importieren", theme.UploadIcon(), func() {
		if !conn.IsConnected() {
			appendLog("✗ Nicht verbunden")
			return
		}
		imp := conn.Importer()
		count := 0
		for i, f := range state.files {
			if !state.checks[i] {
				continue
			}
			// Frisch von Disk lesen damit wir das aktuelle XML haben
			raw, err := os.ReadFile(f.Filename)
			if err != nil {
				// Datei war evtl. nur im Speicher (ParseDSLFile via Pfad)
				raw = []byte(f.RawXML)
			}
			if err := imp.ImportDSL(f.ScreenID, string(raw)); err != nil {
				appendLog(fmt.Sprintf("✗ %s: %v", f.Filename, err))
				continue
			}
			appendLog(fmt.Sprintf("✓ %s → oos.dsl[%s]", f.Filename, f.ScreenID))
			count++
		}
		appendLog(fmt.Sprintf("─── %d DSL-Dateien importiert", count))
	})
	importBtn.Importance = widget.HighImportance

	// ── Linkes Panel zusammenbauen ────────────────────────────────────────
	toolbar := container.NewHBox(chooseBtn, addFileBtn, selectAllBtn, selectNoneBtn, importBtn)
	leftPanel := container.NewBorder(
		container.NewVBox(toolbar, dirLabel),
		container.NewVScroll(logView),
		nil, nil,
		fileList,
	)

	// ── Rechtes Panel (Preview) ───────────────────────────────────────────
	rightPanel := container.NewBorder(
		widget.NewLabel("DSL Preview"),
		nil, nil, nil,
		previewContainer,
	)

	// ── Horizontaler Split ────────────────────────────────────────────────
	split := container.NewHSplit(leftPanel, rightPanel)
	split.SetOffset(0.4)
	return split
}
