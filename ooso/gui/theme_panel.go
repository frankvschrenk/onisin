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

	fynedsl "onisin.com/oos-dsl/dsl"
	oostheme "onisin.com/oos-common/theme"

	"github.com/frankvschrenk/fyne-codeedit"
	"github.com/frankvschrenk/fyne-codeedit/format"
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
	// loadThemeForVariant resolves the theme for a given variant,
	// preferring the row stored in oos.config over the compiled-in
	// default. Returns the default when the database is unreachable
	// or the row is empty so the editor always opens on real
	// colours rather than a blank slate.
	loadThemeForVariant := func(v string) *oostheme.OOSTheme {
		if conn.IsConnected() {
			if xml, err := conn.Importer().LoadThemeXML(v); err == nil && xml != "" {
				if parsed, perr := oostheme.ParseXML(xml); perr == nil {
					return parsed
				}
			}
		}
		return oostheme.DefaultTheme(v)
	}

	xtheme := loadThemeForVariant("light")

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
	//
	// Uses codeedit.ModalEditor so the theme XML shows with syntax
	// highlighting by default. Enter (or click) drops into Edit mode
	// for paste-and-Apply, Escape returns to the coloured preview.
	xmlEditor := codeedit.NewModalEditor(codeedit.LangXML)

	// xmlDirty guards refreshAll from stomping the user's unsaved
	// edits in the XML tab while they are still typing. Flipped on
	// every OnChanged, cleared when the editor writes its own text.
	var xmlDirty bool
	xmlEditor.OnChanged(func(string) { xmlDirty = true })

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
	// oos.config via loadThemeForVariant — light and dark are two
	// independent rows, so this really is a fetch, not just a flag
	// flip on the current theme.
	variantRadio := widget.NewRadioGroup([]string{"light", "dark"}, func(v string) {
		xtheme = loadThemeForVariant(v)
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
	//
	// Same shape as the CTX and DSL panels: a labelled primary Save,
	// a small Reset, and a status label on the right. File import
	// and export were dropped — themes live in oos.config now, and
	// the XML tab is the escape hatch for anyone who needs to
	// copy-paste between environments.
	statusLabel := widget.NewLabel("")

	saveThemeBtn := widget.NewButtonWithIcon("Speichern", theme.DocumentSaveIcon(), func() {
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
	})
	saveThemeBtn.Importance = widget.HighImportance

	// Reset pulls the compiled-in default for the current variant
	// back into the editor. Useful when the user has experimented
	// themselves into a corner and wants a clean starting point
	// without going through the database.
	resetBtn := widget.NewButtonWithIcon("Reset", theme.ContentClearIcon(), func() {
		xtheme = oostheme.DefaultTheme(xtheme.Variant)
		buildEditor(xtheme.ForWidget(kinds[0]))
		widgetList.Select(0)
		variantRadio.SetSelected(xtheme.Variant)
		refreshAll()
		statusLabel.SetText("auf Default zurückgesetzt")
	})

	// container.NewPadded wraps the button row in a theme-padding
	// frame; without it the buttons sit flush against the tab
	// content above and the window edge below.
	toolbar := container.NewPadded(
		container.NewHBox(saveThemeBtn, resetBtn),
	)

	bottomBar := container.NewBorder(nil, nil, nil,
		container.NewPadded(statusLabel),
		toolbar,
	)

	// XML-Tab toolbar: Apply parses the editor content and pushes it
	// back into xtheme, then refreshes the field editor, preview and
	// live-applied Fyne theme. Revert throws away unsaved edits and
	// regenerates the XML from the current xtheme.
	xmlApplyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		parsed, err := oostheme.ParseXML(xmlEditor.Text())
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

	xmlFormatBtn := widget.NewButtonWithIcon("Format", theme.ViewRefreshIcon(), func() {
		src := xmlEditor.Text()
		if src == "" {
			return
		}
		pretty, err := format.FormatXML(src)
		if err != nil {
			statusLabel.SetText("Format: " + err.Error())
			return
		}
		setXMLText(pretty)
		xmlDirty = true
		statusLabel.SetText("formatiert")
	})

	// NewPadded wraps the toolbar in a theme-padding frame so the
	// buttons breathe on all sides instead of sitting flush against
	// the tab strip and the editor. Cheaper than hand-rolling
	// spacers; picks up the theme's Padding size automatically.
	xmlToolbar := container.NewPadded(
		container.NewHBox(xmlApplyBtn, xmlFormatBtn, xmlRevertBtn),
	)
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
