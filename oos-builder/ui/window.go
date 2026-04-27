// window.go — public entry point used by host applications (ooso in
// the first instance). The visible API surface is intentionally
// minimal: a single OpenWindow(app, cfg) function that mirrors
// oos-chat/chat.OpenWindow.
//
// Lives in package ui (not package builder) because it composes the
// ui panels — keeping it in builder would force ui -> builder ->
// ui as a cycle.

package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-builder/builder"
	"onisin.com/oos-builder/schema"
)

// Config bundles every external dependency the builder window needs.
//
// OOSPBaseURL points at a running oosp instance — the grammar is
// fetched from it on Open. InitialXML is the document the window
// should load (empty means "fresh skeleton"). RootID is the screen
// id used when InitialXML is empty.
//
// OnXML is called when the user clicks "Übernehmen". The host is
// expected to write the XML somewhere — typically into the editor
// buffer in the host's DSL panel — so the builder window stays a
// dumb editor that only knows about its in-memory tree.
type Config struct {
	OOSPBaseURL string
	InitialXML  string
	RootID      string
	OnXML       func(xml string) error
}

// OpenWindow constructs and shows a new builder window. The grammar
// fetch is synchronous to keep the wiring simple — typical fetch is
// well under 50 ms against localhost. If oosp is unreachable the
// function returns an error and no window appears; the caller is
// expected to surface that to the user.
func OpenWindow(app fyne.App, cfg Config) (fyne.Window, error) {
	if cfg.OOSPBaseURL == "" {
		cfg.OOSPBaseURL = os.Getenv("OOSP_URL")
		if cfg.OOSPBaseURL == "" {
			cfg.OOSPBaseURL = "http://localhost:9100"
		}
	}

	// Pull the grammar once at open time. Cached for the lifetime of
	// this window — the user can close and re-open to pick up changes.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	xsd, err := schema.NewFetcher(cfg.OOSPBaseURL).FetchGrammar(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch grammar: %w", err)
	}
	cat, err := schema.ParseCatalog(xsd)
	if err != nil {
		return nil, fmt.Errorf("parse grammar: %w", err)
	}

	tree, err := builder.LoadXML(cfg.InitialXML, cfg.RootID)
	if err != nil {
		return nil, fmt.Errorf("load initial xml: %w", err)
	}

	w := app.NewWindow("OOS Builder — " + tree.Root.Attrs["id"])
	w.Resize(fyne.NewSize(1400, 900))

	// Build the three columns.
	palette := NewPalette(tree, cat)
	canvas := NewCanvas(tree)
	treePanel := NewTreePanel(tree)
	props := NewPropertiesPanel(tree, cat)

	// Right column: tree on top, properties below, with a vertical
	// split so the user can rebalance.
	rightSplit := container.NewVSplit(treePanel, props)
	rightSplit.SetOffset(0.4)

	// Outer layout: palette | canvas | right column. Two horizontal
	// splits, palette pinned narrow, right column reasonable, canvas
	// gets the rest.
	mid := container.NewHSplit(canvas, rightSplit)
	mid.SetOffset(0.65)

	main := container.NewHSplit(palette, mid)
	main.SetOffset(0.18)

	// Header / toolbar.
	statusLabel := widget.NewLabel(fmt.Sprintf(
		"%d Elemente geladen — Wurzel: %s",
		len(cat.Elements), cat.RootName,
	))
	applyBtn := widget.NewButtonWithIcon("Übernehmen", theme.ConfirmIcon(), func() {
		if cfg.OnXML == nil {
			statusLabel.SetText("Kein OnXML-Callback konfiguriert.")
			return
		}
		out, err := tree.MarshalXML()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if err := cfg.OnXML(out); err != nil {
			dialog.ShowError(err, w)
			return
		}
		statusLabel.SetText("XML übernommen.")
	})
	applyBtn.Importance = widget.HighImportance

	closeBtn := widget.NewButtonWithIcon("Schließen", theme.CancelIcon(), func() {
		w.Close()
	})

	header := container.NewBorder(
		nil, nil,
		container.NewHBox(applyBtn, closeBtn),
		nil,
		statusLabel,
	)

	w.SetContent(container.NewBorder(header, nil, nil, nil, main))
	return w, nil
}
