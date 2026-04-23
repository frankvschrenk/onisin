package gui

// theme_panel.go — OOS Theme Editor.
//
// buildThemePanel gibt zurück:
//   - fyne.CanvasObject: Editor-Panel (Tabs: Editor + XML)
//   - func(): öffnet/fokussiert das Preview-Fenster
//
// Das Preview-Fenster öffnet sich automatisch wenn "Theme" in der Sidebar
// gewählt wird. Es bleibt offen und aktualisiert sich bei jeder Änderung.

import (
	"bytes"
	"fmt"
	"image/color"
	"io"

	fynedsl "onisin.com/oos-dsl/dsl"
	oostheme "onisin.com/oos-common/theme"

	"github.com/lusingander/colorpicker"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// buildThemePanel baut den Theme-Editor und gibt ihn zusammen mit
// einer openPreview-Funktion zurück.
func buildThemePanel(conn *Connection) (fyne.CanvasObject, func()) {
	// Fenster lazy holen — erst beim Aufruf verfügbar, nicht beim Panel-Bauen
	getWindow := func() fyne.Window {
		return fyne.CurrentApp().Driver().AllWindows()[0]
	}
	xtheme := oostheme.DefaultTheme("light")

	// ── Preview-Fenster ───────────────────────────────────────────────────
	var previewWin fyne.Window
	previewArea := container.NewStack(widget.NewLabel("← Screen wählen"))

	screenNames := make([]string, len(testScreens))
	for i, s := range testScreens {
		screenNames[i] = s.name
	}

	var currentScreen *testScreen

	refreshPreview := func() {
		if currentScreen == nil {
			return
		}
		root, err := fynedsl.Parse(bytes.NewReader(currentScreen.dsl))
		if err != nil {
			previewArea.Objects = []fyne.CanvasObject{widget.NewLabel("Fehler: " + err.Error())}
			previewArea.Refresh()
			return
		}
		state := fynedsl.NewState()
		if len(currentScreen.data) > 0 {
			_ = state.LoadJSON(currentScreen.data)
		}
		builder := fynedsl.NewBuilder(state, currentScreen.name, nil)
		builder.SetThemeProvider(oostheme.NewAdapter(xtheme))
		screen := builder.Build(root)
		previewArea.Objects = []fyne.CanvasObject{container.NewVScroll(screen)}
		previewArea.Refresh()
	}

	screenSelect := widget.NewSelect(screenNames, func(name string) {
		for i := range testScreens {
			if testScreens[i].name == name {
				currentScreen = &testScreens[i]
				refreshPreview()
				return
			}
		}
	})

	// openPreview öffnet das Preview-Fenster (oder fokussiert es).
	// Wird von app.go aufgerufen wenn "Theme" in der Sidebar gewählt wird.
	openPreview := func() {
		if previewWin != nil {
			previewWin.Show()
			previewWin.RequestFocus()
			return
		}
		previewWin = fyne.CurrentApp().NewWindow("Theme Preview")
		previewWin.Resize(fyne.NewSize(900, 700))
		previewWin.SetContent(container.NewBorder(
			container.NewVBox(
				container.NewHBox(
					widget.NewLabel("Screen:"),
					screenSelect,
				),
				widget.NewSeparator(),
			),
			nil, nil, nil,
			previewArea,
		))
		previewWin.SetOnClosed(func() { previewWin = nil })
		previewWin.Show()
	}

	// ── Tab 2: XML ────────────────────────────────────────────────────────
	//
	// The XML tab is now editable. Paste a theme XML, hit "Apply",
	// and the field-by-field editor on the other tab plus the preview
	// window update to match. Also works the other way round: edits
	// in the editor tab regenerate the XML here on every refreshAll.
	xmlEditor := widget.NewMultiLineEntry()
	xmlEditor.Wrapping = fyne.TextWrapOff
	xmlEditor.SetPlaceHolder("<oos-theme>...</oos-theme>")

	// xmlDirty guards refreshAll from stomping the user's unsaved
	// edits in the XML tab while they are still typing. Flipped on
	// every OnChanged, cleared when the editor writes its own text.
	var xmlDirty bool
	xmlEditor.OnChanged = func(string) { xmlDirty = true }

	setXMLText := func(xml string) {
		xmlEditor.SetText(xml)
		xmlDirty = false
	}

	// refreshAll: regenerate XML from xtheme, re-apply theme, redraw
	// preview. Safe to call before the Fyne app has fully started —
	// the CurrentApp guard falls through silently.
	refreshAll := func() {
		if !xmlDirty {
			if xml, err := xtheme.ToXML(); err == nil {
				setXMLText(xml)
			} else {
				setXMLText("Fehler: " + err.Error())
			}
		}
		if a := fyne.CurrentApp(); a != nil {
			a.Settings().SetTheme(oostheme.NewGlobalFyneTheme(xtheme))
		}
		refreshPreview()
	}

	if xml, err := xtheme.ToXML(); err == nil {
		setXMLText(xml)
	}

	// ── Eigenschafts-Editor ───────────────────────────────────────────────
	editorTitle := widget.NewLabelWithStyle(
		"", fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)
	editorRows := container.NewVBox()

	buildEditor := func(wt *oostheme.WidgetTheme) {
		editorTitle.SetText(string(wt.Kind))
		editorRows.Objects = nil

		colorRow := func(label string, getVal func() string, setVal func(string)) fyne.CanvasObject {
			hexLabel := widget.NewLabelWithStyle(
				getVal(), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true},
			)
			// Button öffnet bei Klick einen Picker-Dialog — komplett lazy
			// kein fyne.CurrentApp() beim Panel-Bauen
			openBtn := widget.NewButton("...", func() {
				picker := colorpicker.New(220, colorpicker.StyleHue)
				c := oostheme.ParseHex(hexLabel.Text)
				if c == color.Transparent {
					c = color.NRGBA{0x28, 0x29, 0x2e, 0xff}
				}
				picker.SetColor(c)
				picker.SetOnChanged(func(c color.Color) {
					hex := oostheme.ToHex(c)
					hexLabel.SetText(hex)
					setVal(hex)
					refreshAll()
				})
				dialog.ShowCustom("Farbe wählen", "OK",
					container.NewCenter(picker),
					getWindow())
			})
			return container.New(layout.NewGridLayoutWithColumns(3),
				widget.NewLabel(label), hexLabel, openBtn,
			)
		}

		sizeRow := func(label string, getVal func() string, setVal func(string)) fyne.CanvasObject {
			entry := widget.NewEntry()
			entry.SetPlaceHolder("—")
			entry.SetText(getVal())
			entry.OnChanged = func(v string) {
				setVal(v)
				refreshAll()
			}
			return container.New(layout.NewGridLayoutWithColumns(2),
				widget.NewLabel(label), entry,
			)
		}

		editorRows.Add(colorRow("Background",
			func() string { return wt.Background },
			func(v string) { wt.Background = v }))
		editorRows.Add(colorRow("Foreground",
			func() string { return wt.Foreground },
			func(v string) { wt.Foreground = v }))
		editorRows.Add(colorRow("Primary",
			func() string { return wt.Primary },
			func(v string) { wt.Primary = v }))
		editorRows.Add(colorRow("Border",
			func() string { return wt.Border },
			func(v string) { wt.Border = v }))

		if wt.Kind == oostheme.KindTable || wt.Kind == oostheme.KindList {
			editorRows.Add(colorRow("Header",
				func() string { return wt.Header },
				func(v string) { wt.Header = v }))
		}

		editorRows.Add(widget.NewSeparator())
		editorRows.Add(sizeRow("Text Size",
			func() string { return wt.TextSize },
			func(v string) { wt.TextSize = v }))
		editorRows.Add(sizeRow("Radius",
			func() string { return wt.Radius },
			func(v string) { wt.Radius = v }))
		editorRows.Add(sizeRow("Padding",
			func() string { return wt.Padding },
			func(v string) { wt.Padding = v }))

		editorRows.Refresh()
	}

	// ── Links: Widget-Liste ───────────────────────────────────────────────
	kinds := oostheme.AllWidgetKinds
	widgetList := widget.NewList(
		func() int { return len(kinds) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(string(kinds[id]))
		},
	)
	widgetList.OnSelected = func(id widget.ListItemID) {
		buildEditor(xtheme.ForWidget(kinds[id]))
	}
	widgetList.Select(0)
	buildEditor(xtheme.ForWidget(kinds[0]))

	// Switching the variant reloads the corresponding theme row from
	// oos.config; light and dark are two independent rows, so this
	// really is a fetch, not just a flag flip on the current theme.
	// If the row is missing, we fall back to the compiled-in default
	// for that variant so the editor always starts on real colours.
	variantRadio := widget.NewRadioGroup([]string{"light", "dark"}, func(v string) {
		loaded := oostheme.DefaultTheme(v)
		if conn.IsConnected() {
			if xml, err := conn.Importer().LoadThemeXML(v); err == nil && xml != "" {
				if parsed, perr := oostheme.ParseXML(xml); perr == nil {
					loaded = parsed
				}
			}
		}
		xtheme = loaded
		buildEditor(xtheme.ForWidget(kinds[0]))
		widgetList.Select(0)
		refreshAll()
	})
	variantRadio.SetSelected("light")
	variantRadio.Horizontal = true

	leftPanel := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Variante:"),
			variantRadio,
			widget.NewSeparator(),
		),
		nil, nil, nil,
		widgetList,
	)

	rightPanel := container.NewBorder(
		container.NewVBox(editorTitle, widget.NewSeparator()),
		nil, nil, nil,
		container.NewVScroll(editorRows),
	)

	editorSplit := container.NewHSplit(leftPanel, rightPanel)
	editorSplit.SetOffset(0.25)

	// ── Toolbar ───────────────────────────────────────────────────────────
	statusLabel := widget.NewLabel("")

	toolbar := widget.NewToolbar(
		// Aus Datei laden
		widget.NewToolbarAction(theme.FolderOpenIcon(), func() {
			dialog.ShowFileOpen(func(f fyne.URIReadCloser, err error) {
				if err != nil || f == nil {
					return
				}
				defer f.Close()
				data, err := io.ReadAll(f)
				if err != nil {
					statusLabel.SetText("Lesen: " + err.Error())
					return
				}
				parsed, err := oostheme.ParseXML(string(data))
				if err != nil {
					statusLabel.SetText("XML: " + err.Error())
					return
				}
				*xtheme = *parsed
				buildEditor(xtheme.ForWidget(kinds[0]))
				widgetList.Select(0)
				variantRadio.SetSelected(xtheme.Variant)
				refreshAll()
				statusLabel.SetText("geladen: " + f.URI().Name())
			}, getWindow())
		}),
		// Als Datei speichern
		widget.NewToolbarAction(theme.DocumentSaveIcon(), func() {
			xml, err := xtheme.ToXML()
			if err != nil {
				statusLabel.SetText(err.Error())
				return
			}
			dialog.ShowFileSave(func(f fyne.URIWriteCloser, err error) {
				if err != nil || f == nil {
					return
				}
				defer f.Close()
				if _, err := f.Write([]byte(xml)); err != nil {
					statusLabel.SetText("Schreiben: " + err.Error())
					return
				}
				statusLabel.SetText("gespeichert: " + f.URI().Name())
			}, getWindow())
		}),
		widget.NewToolbarSeparator(),
		// Save into oos.config under theme.<variant>. The variant in
		// the theme's own <oos-theme variant="..."> attribute decides
		// which row we target, so the editor writes where the desktop
		// client will later read.
		widget.NewToolbarAction(theme.UploadIcon(), func() {
			if !conn.IsConnected() {
				statusLabel.SetText("nicht verbunden")
				return
			}
			xml, err := xtheme.ToXML()
			if err != nil {
				statusLabel.SetText(err.Error())
				return
			}
			if err := conn.Importer().ImportThemeXML(xtheme.Variant, xml); err != nil {
				statusLabel.SetText(err.Error())
				return
			}
			statusLabel.SetText(fmt.Sprintf("gespeichert → oos.config[theme.%s]", xtheme.Variant))
		}),
		widget.NewToolbarSeparator(),
		// Reset to the compiled-in default for the currently-selected
		// variant. Keeps the radio selection so "reset dark" stays dark.
		widget.NewToolbarAction(theme.ContentClearIcon(), func() {
			xtheme = oostheme.DefaultTheme(xtheme.Variant)
			buildEditor(xtheme.ForWidget(kinds[0]))
			widgetList.Select(0)
			variantRadio.SetSelected(xtheme.Variant)
			refreshAll()
			statusLabel.SetText("")
		}),
		widget.NewToolbarSpacer(),
	)

	bottomBar := container.NewBorder(nil, nil, nil, statusLabel, toolbar)

	// XML-Tab toolbar: Apply parses the editor content and pushes it
	// back into xtheme, then refreshes the field editor, preview and
	// live-applied Fyne theme. Revert throws away unsaved edits and
	// regenerates the XML from the current xtheme.
	xmlApplyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		parsed, err := oostheme.ParseXML(xmlEditor.Text)
		if err != nil {
			statusLabel.SetText("XML: " + err.Error())
			return
		}
		xtheme = parsed
		xmlDirty = false
		buildEditor(xtheme.ForWidget(kinds[0]))
		widgetList.Select(0)
		variantRadio.SetSelected(xtheme.Variant)
		refreshAll()
		statusLabel.SetText("XML übernommen")
	})
	xmlApplyBtn.Importance = widget.HighImportance

	xmlRevertBtn := widget.NewButtonWithIcon("Revert", theme.ContentUndoIcon(), func() {
		if xml, err := xtheme.ToXML(); err == nil {
			setXMLText(xml)
			statusLabel.SetText("")
		}
	})

	xmlToolbar := container.NewHBox(xmlApplyBtn, xmlRevertBtn)
	xmlTab := container.NewBorder(xmlToolbar, nil, nil, nil, xmlEditor)

	// ── Tabs ──────────────────────────────────────────────────────────────
	tabs := container.NewAppTabs(
		container.NewTabItem("Editor", editorSplit),
		container.NewTabItem("XML", xmlTab),
	)
	tabs.OnSelected = func(tab *container.TabItem) {
		if tab.Text == "XML" {
			refreshAll()
		}
	}

	panel := container.NewBorder(nil, bottomBar, nil, nil, tabs)
	return panel, openPreview
}
