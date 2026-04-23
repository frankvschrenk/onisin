package gui

// ctx_panel.go — Panel zum Importieren von CTX-Dateien.
//
// Layout:
//   [ groups.xml wählen  ]  [ Import-Button ]
//   [ Gruppen-Liste      ]  ← checkbare Liste
//   [ Log-Ausgabe        ]  ← Ergebnis + Fehler
//
// Der Benutzer wählt eine groups.xml Datei.
// Die Gruppen darin werden aufgelistet.
// Einzelne Gruppen oder alle können importiert werden.

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"onisin.com/oos-common/importer"
)

// ctxPanelState hält den Zustand des CTX-Panels.
type ctxPanelState struct {
	conn       *Connection
	groupsPath string
	groups     []importer.Group
	checks     []bool // welche Gruppen ausgewählt sind
}

// buildCTXPanel baut das CTX-Import Panel.
func buildCTXPanel(conn *Connection) fyne.CanvasObject {
	state := &ctxPanelState{conn: conn}

	// Pfad-Anzeige
	pathLabel := widget.NewLabel("keine groups.xml gewählt")
	pathLabel.Wrapping = fyne.TextTruncate

	// Gruppen-Liste
	groupList := widget.NewList(
		func() int { return len(state.groups) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewCheck("", nil), widget.NewLabel(""))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			check := row.Objects[0].(*widget.Check)
			label := row.Objects[1].(*widget.Label)
			if id >= len(state.groups) {
				return
			}
			g := state.groups[id]
			label.SetText(fmt.Sprintf("%s  [%s]  — %d Dateien",
				g.Name, g.Role, len(g.Includes)))
			check.SetChecked(state.checks[id])
			check.OnChanged = func(checked bool) {
				state.checks[id] = checked
			}
		},
	)

	// Log-Ausgabe
	logView := widget.NewMultiLineEntry()
	logView.Wrapping = fyne.TextWrapWord
	logView.Disable()

	appendLog := func(msg string) {
		current := logView.Text
		if current != "" {
			current += "\n"
		}
		logView.SetText(current + msg)
	}

	// groups.xml Datei wählen
	chooseBtn := widget.NewButtonWithIcon("groups.xml wählen…", theme.FolderOpenIcon(), func() {
		w := fyne.CurrentApp().Driver().AllWindows()[0]
		dialog.ShowFileOpen(func(f fyne.URIReadCloser, err error) {
			if err != nil || f == nil {
				return
			}
			defer f.Close()
			path := f.URI().Path()
			groups, err := importer.ParseGroupsFile(path)
			if err != nil {
				appendLog("Fehler: " + err.Error())
				return
			}
			state.groupsPath = path
			state.groups = groups
			state.checks = make([]bool, len(groups))
			// Alle vorauswählen
			for i := range state.checks {
				state.checks[i] = true
			}
			pathLabel.SetText(path)
			groupList.Refresh()
			appendLog(fmt.Sprintf("✓ %d Gruppen geladen aus %s",
				len(groups), filepath.Base(path)))
		}, w)
	})

	// Alle auswählen / abwählen
	selectAllBtn := widget.NewButtonWithIcon("Alle", theme.CheckButtonCheckedIcon(), func() {
		for i := range state.checks {
			state.checks[i] = true
		}
		groupList.Refresh()
	})

	selectNoneBtn := widget.NewButtonWithIcon("Keine", theme.CheckButtonIcon(), func() {
		for i := range state.checks {
			state.checks[i] = false
		}
		groupList.Refresh()
	})

	// Import ausführen
	importBtn := widget.NewButtonWithIcon("Importieren", theme.UploadIcon(), func() {
		if !conn.IsConnected() {
			appendLog("✗ Nicht verbunden — bitte zuerst Verbinden")
			return
		}
		if state.groupsPath == "" {
			appendLog("✗ Keine groups.xml gewählt")
			return
		}

		imp := conn.Importer()
		ctxDir := filepath.Dir(state.groupsPath)

		// groups.xml selbst importieren
		groupsXML, err := os.ReadFile(state.groupsPath)
		if err != nil {
			appendLog("✗ groups.xml lesen: " + err.Error())
			return
		}
		if err := imp.ImportGroupsFile(string(groupsXML)); err != nil {
			appendLog("✗ groups.xml: " + err.Error())
			return
		}

		// Ausgewählte Gruppen importieren
		imported := 0
		for i, g := range state.groups {
			if !state.checks[i] {
				continue
			}
			files, err := readGroupFiles(g, ctxDir)
			if err != nil {
				appendLog(fmt.Sprintf("✗ Gruppe %s: %v", g.Name, err))
				continue
			}
			if err := imp.ImportGroup(g.Name, files); err != nil {
				appendLog(fmt.Sprintf("✗ Gruppe %s: %v", g.Name, err))
				continue
			}
			appendLog(fmt.Sprintf("✓ Gruppe %s (%d Dateien)", g.Name, len(files)))
			imported++
		}
		appendLog(fmt.Sprintf("─── %d Gruppen importiert", imported))
	})
	importBtn.Importance = widget.HighImportance

	// Layout zusammenbauen
	toolbar := container.NewHBox(chooseBtn, selectAllBtn, selectNoneBtn, importBtn)
	header := container.NewVBox(toolbar, pathLabel)

	return container.NewBorder(
		header,
		container.NewVScroll(logView),
		nil, nil,
		groupList,
	)
}

// readGroupFiles liest alle CTX-Dateien einer Gruppe.
func readGroupFiles(g importer.Group, ctxDir string) (map[string]string, error) {
	files := make(map[string]string)
	for _, inc := range g.Includes {
		data, err := os.ReadFile(filepath.Join(ctxDir, inc))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inc, err)
		}
		files[inc] = string(data)
	}
	return files, nil
}
