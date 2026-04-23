package ctx

// preview.go — ooso dsl preview
//
// Öffnet ein Fyne-Fenster das die DSL live rendert.
// Beim Speichern der DSL- oder JSON-Datei wird der Screen automatisch neu aufgebaut.
//
// Aufruf:
//   ooso dsl preview --dsl person-detail.dsl.xml --data person-detail.json
//
// Ablauf:
//   1. DSL-Datei einlesen und via oos-dsl parsen → Node-Baum
//   2. JSON-Datei einlesen und State laden
//   3. Fyne-Fenster mit dem gerenderten Screen anzeigen
//   4. fsnotify Watcher auf beide Dateien starten
//   5. Bei Änderung → neu parsen und Fenster aktualisieren (fyne.Do)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"onisin.com/oos-dsl/dsl"
)

// newDSLPreviewCommand gibt den cobra.Command für "ooso dsl preview" zurück.
func newDSLPreviewCommand() *cobra.Command {
	var dslFile string
	var dataFile string

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "DSL-Datei live in einem Fyne-Fenster rendern",
		Long: `Rendert eine x-DSL Datei live im Fyne-Fenster.
Ändert sich die DSL- oder JSON-Datei, wird der Screen sofort neu aufgebaut.
Kein Datenbankzugriff — nur lokale Dateien.

Beispiel:
  ooso dsl preview --dsl ./dsl/person-detail.dsl.xml --data ./data/person.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dslFile == "" {
				return fmt.Errorf("--dsl fehlt")
			}
			return runPreview(dslFile, dataFile)
		},
	}

	cmd.Flags().StringVar(&dslFile, "dsl", "", "Pfad zur *.dsl.xml Datei (Pflicht)")
	cmd.Flags().StringVar(&dataFile, "data", "", "Pfad zur JSON-Datendatei (optional)")

	return cmd
}

// ── Preview Runner ────────────────────────────────────────────────────────────

// runPreview startet das Fyne-Fenster und den File Watcher.
func runPreview(dslPath, dataPath string) error {
	a := app.New()
	w := a.NewWindow("ooso — DSL Preview")
	w.Resize(fyne.NewSize(960, 680))

	// Initialer Render
	content, err := buildPreviewContent(dslPath, dataPath)
	if err != nil {
		// Fehler sofort anzeigen — kein Abbruch
		log.Printf("[preview] erster render: %v", err)
		content = errorWidget(err)
	}
	w.SetContent(content)

	// File Watcher starten — läuft im Hintergrund
	stopWatcher := startWatcher(dslPath, dataPath, func() {
		// Callback läuft im Watcher-Goroutine → Fyne-Update via fyne.Do
		newContent, err := buildPreviewContent(dslPath, dataPath)
		if err != nil {
			log.Printf("[preview] reload: %v", err)
			newContent = errorWidget(err)
		}
		fyne.Do(func() {
			w.SetContent(newContent)
			w.Canvas().Refresh(w.Content())
		})
	})
	defer stopWatcher()

	w.ShowAndRun()
	return nil
}

// ── Content Builder ───────────────────────────────────────────────────────────

// buildPreviewContent liest DSL + JSON und gibt den gerenderten Fyne-Container zurück.
func buildPreviewContent(dslPath, dataPath string) (fyne.CanvasObject, error) {
	// 1. DSL-Datei lesen und parsen
	f, err := os.Open(dslPath)
	if err != nil {
		return nil, fmt.Errorf("dsl lesen: %w", err)
	}
	defer f.Close()

	rootNode, err := dsl.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("dsl parse: %w", err)
	}

	// 2. JSON-State laden
	state := dsl.NewState()
	if dataPath != "" {
		data, err := os.ReadFile(dataPath)
		if err != nil {
			return nil, fmt.Errorf("json lesen: %w", err)
		}
		if err := state.LoadJSON(data); err != nil {
			return nil, fmt.Errorf("json parse: %w", err)
		}
	}

	// 3. Screen-Titel und ID aus DSL-Wurzel
	title := rootNode.Attr("title", "DSL Preview")
	screenID := rootNode.Attr("id", "preview")

	// 4. Event-Log Widget (zeigt ausgelöste Actions)
	eventLog := widget.NewMultiLineEntry()
	eventLog.SetPlaceHolder("→ Actions erscheinen hier nach Klick")
	eventLog.Wrapping = fyne.TextWrapWord
	eventLog.SetMinRowsVisible(4)

	onAction := func(sid, action string, s *dsl.State) {
		b, err := dsl.BuildEvent(sid, action, s)
		if err != nil {
			eventLog.SetText(fmt.Sprintf("fehler: %v", err))
			return
		}
		eventLog.SetText(string(b))

		// Auch als JSON auf stdout — praktisch für Debugging
		var pretty map[string]any
		if err := json.Unmarshal(b, &pretty); err == nil {
			out, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(out))
		}
	}

	// 5. Screen rendern
	builder := dsl.NewBuilder(state, screenID, onAction)
	screen := builder.Build(rootNode)

	// 6. Layout: Screen oben, Event-Log unten
	// scroll="true" am <screen> → vertikaler Scroll
	var screenWidget fyne.CanvasObject
	if rootNode.Attr("scroll", "") == "true" {
		screenWidget = container.NewVScroll(screen)
	} else {
		screenWidget = screen
	}

	content := container.NewBorder(
		widget.NewLabel("📄 "+title),
		container.NewVScroll(eventLog),
		nil, nil,
		screenWidget,
	)

	_ = screenID
	return content, nil
}

// errorWidget zeigt einen Parse-Fehler im Fenster an — kein Crash.
func errorWidget(err error) fyne.CanvasObject {
	label := widget.NewLabel("⚠ Fehler:\n" + err.Error())
	label.Wrapping = fyne.TextWrapWord
	return label
}

// ── File Watcher ──────────────────────────────────────────────────────────────

// startWatcher überwacht dslPath und optional dataPath.
// Ruft onChange auf sobald eine der Dateien gespeichert wird.
// Gibt eine Stop-Funktion zurück.
func startWatcher(dslPath, dataPath string, onChange func()) func() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[watcher] konnte nicht gestartet werden: %v", err)
		return func() {}
	}

	if err := watcher.Add(dslPath); err != nil {
		log.Printf("[watcher] dsl: %v", err)
	}
	if dataPath != "" {
		if err := watcher.Add(dataPath); err != nil {
			log.Printf("[watcher] data: %v", err)
		}
	}

	go watchLoop(watcher, onChange)

	log.Printf("[watcher] beobachtet: %s", dslPath)
	if dataPath != "" {
		log.Printf("[watcher] beobachtet: %s", dataPath)
	}

	return func() { watcher.Close() }
}

// watchLoop läuft in einer Goroutine und ruft onChange bei Write-Events auf.
// Mehrfach-Events (z.B. durch Editor-Autosave) werden durch einfaches
// Weiterreichen toleriert — kein Debounce nötig da Fyne-Render schnell ist.
func watchLoop(watcher *fsnotify.Watcher, onChange func()) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				log.Printf("[watcher] geändert: %s", event.Name)
				onChange()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[watcher] fehler: %v", err)
		}
	}
}
