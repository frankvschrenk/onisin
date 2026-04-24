// Command demo launches a small Fyne window with a CodeEditor bound
// to an XML snippet so the editor-style shortcuts can be exercised
// by hand. Run with:
//
//	go run ./cmd/demo
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/frankvschrenk/fyne-codeedit"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<oos>
  <context name="person_list" kind="collection" source="person">
    <permission role="admin"   actions="read,write,delete"/>
    <permission role="manager" actions="read,write"/>
    <permission role="user"    actions="read"/>
    <list_fields>id, firstname, lastname, email</list_fields>
    <field name="id"        type="int"    readonly="true"/>
    <field name="firstname" type="string" filterable="true"/>
    <field name="lastname"  type="string" filterable="true"/>
  </context>
</oos>
`

func main() {
	a := app.NewWithID("com.frankvschrenk.fyne-codeedit.demo")
	w := a.NewWindow("fyne-codeedit — Demo")
	w.Resize(fyne.NewSize(800, 600))

	editor := codeedit.NewWithLanguage(codeedit.LangXML)
	editor.SetText(sampleXML)

	help := widget.NewLabel(
		"Tab / Shift+Tab — indent / dedent\n" +
			"Enter — auto-indent from current line\n" +
			"Ctrl+D — delete current line\n" +
			"Ctrl+L — insert line above\n" +
			"Ctrl+K — toggle XML comment",
	)

	w.SetContent(container.NewBorder(nil, help, nil, nil, editor))
	w.ShowAndRun()
}
