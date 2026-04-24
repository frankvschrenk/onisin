// Command demo launches a single ModalEditor — two stages stacked:
//
//   - Preview: chroma-highlighted, read-only
//   - Edit:    plain CodeEditor with editor shortcuts
//
// Press Enter in Preview to switch to Edit; press Escape in Edit to
// switch back to Preview. A badge in the top-right corner reports
// which stage is active.
//
// Run with:
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
	w.Resize(fyne.NewSize(900, 600))

	editor := codeedit.NewModalEditor(codeedit.LangXML)
	editor.SetText(sampleXML)

	help := widget.NewLabel(
		"Preview: Enter → Edit.    Edit: Escape → Preview.    " +
			"Tab/Shift+Tab — indent/dedent.    " +
			"Ctrl+D — delete line.    Ctrl+L — insert line above.    " +
			"Ctrl+K — toggle comment.",
	)

	w.SetContent(container.NewBorder(nil, help, nil, nil, editor))
	w.ShowAndRun()
}
