// main.go — DSL Proof of Concept
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-dsl/dsl"
)

func main() {
	dslFile := flag.String("dsl", "testdata/user-profile.xml", "Pfad zur DSL XML-Datei")
	dataFile := flag.String("data", "", "Pfad zur JSON-Datei")
	flag.Parse()

	a := app.New()
	w := a.NewWindow("OOS DSL Demo")
	w.Resize(fyne.NewSize(960, 680))

	state := dsl.NewState()
	if *dataFile != "" {
		data, err := os.ReadFile(*dataFile)
		if err != nil {
			log.Fatalf("JSON lesen: %v", err)
		}
		if err := state.LoadJSON(data); err != nil {
			log.Fatalf("JSON parse: %v", err)
		}
	}

	f, err := os.Open(*dslFile)
	if err != nil {
		log.Fatalf("DSL lesen: %v", err)
	}
	defer f.Close()

	rootNode, err := dsl.Parse(f)
	if err != nil {
		log.Fatalf("DSL parse: %v", err)
	}

	w.SetTitle(rootNode.Attr("title", "OOS DSL Demo"))
	screenID := rootNode.Attr("id", "screen")

	jsonOutput := widget.NewMultiLineEntry()
	jsonOutput.SetPlaceHolder("→ JSON-Event erscheint hier nach Aktion")
	jsonOutput.Wrapping = fyne.TextWrapWord
	jsonOutput.SetMinRowsVisible(8)

	onAction := func(sid, action string, s *dsl.State) {
		jsonBytes, err := dsl.BuildEvent(sid, action, s)
		if err != nil {
			jsonOutput.SetText(fmt.Sprintf("Fehler: %v", err))
			return
		}
		output := string(jsonBytes)
		jsonOutput.SetText(output)
		fmt.Println(output)
	}

	builder := dsl.NewBuilder(state, screenID, onAction)
	screen := builder.Build(rootNode)

	// scroll="true" am <screen> → vertikaler Scroll
	var content fyne.CanvasObject
	if rootNode.Attr("scroll", "") == "true" {
		content = container.NewBorder(
			nil, container.NewScroll(jsonOutput),
			nil, nil,
			container.NewVScroll(screen),
		)
	} else {
		content = container.NewBorder(
			nil, container.NewScroll(jsonOutput),
			nil, nil,
			screen,
		)
	}

	w.SetContent(content)

	// Fokus setzen — muss nach SetContent() kommen da der Canvas erst dann aktiv ist
	if ft := builder.FocusTarget(); ft != nil {
		w.Canvas().Focus(ft)
	}

	w.ShowAndRun()
}
