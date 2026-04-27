# oos-builder — experimentell, derzeit nicht genutzt

**Status:** Eingefroren. Nicht aktiv eingebunden, nicht in `make compile`.

## Was hier liegt

Tag-1-Skelett eines visuellen DSL-Builders. Drei Schichten:

- `schema/` — fetcht `oos.oos_dsl_meta` (namespace `grammar`) von oosp
  über `GET /dsl/meta`, parst die XSD zu einem `Catalog{Elements,
  Attrs, Children, Category}`. Tests gegen die echte `dsl.xsd`.
- `builder/` — Tree-Modell um `*base.Node` mit Mutations
  (`AppendChild`, `Remove`, `MoveBefore`, `SetAttr`, `Select`),
  Listener-Pattern (`OnChange`), stabilem `MarshalXML`. Tests grün.
- `ui/` — Fyne-Widgets: Palette, Canvas (Live-Preview über
  `dsl.NewBuilder`), Tree-Panel, Properties-Panel, plus
  `OpenWindow(app, Config)` als Einstieg.

## Warum eingefroren

Der erste Live-Test im April 2026 hat gezeigt:

1. **Die Architektur ist UI-instabil.** Properties-Edits triggern
   `notify()`, das die Properties-Felder selbst rebuildet — Fokus-
   Verlust und Tipp-Loops sind die Folge. Reparierbar, aber
   symptomatisch.
2. **Click-Insert ist nicht RAD.** Echtes RAD heißt Pixel-Drag,
   Resize-Handles, freie Canvas — und das ist mit Fyne nicht in
   Tagen, sondern in Wochen Eigenbau zu haben.
3. **Der Mehrwert über den XML-Editor ist gering.** Frank hat
   diagnostiziert: lieber direkt im XML arbeiten als ein halbgares
   Visual.

Die Diskussion und die Schlussfolgerungen stehen ausführlicher in
`.claude/session.md` (Stand 27. April 2026).

## Was wertvoll bleibt

Das Datenmodell und der XSD-Parser sind beide eigenständig nützlich
und unabhängig vom UI:

- Wenn irgendwann ein **stärkerer XML-Editor** mit XSD-getriebenem
  Auto-Complete in ooso eingebaut wird, ist `schema.ParseCatalog`
  der bereits getestete Lieferant für die Strukturinformation
  (welche Children erlaubt? welche Attribute? welche Enum-Werte?).
- Wenn irgendwann doch ein RAD-Inselprojekt entsteht (Qt? Web?),
  ist `builder.Tree` mit `MarshalXML` ein sauberer Kern, der nur
  ein anderes Frontend braucht.

## Was vom Builder nach oosp gewandert ist

Der Endpoint `GET /dsl/meta?ns={grammar|enrichment}` in `oosp/api`
bleibt. Er liefert die XSD bzw. Enrichment direkt aus
`oos.oos_dsl_meta` als `application/xml`. Ist sauber, klein, ohne
Abhängigkeit auf den Builder, und für jedes künftige Werkzeug
(XML-Editor, RAD, externes Tool) die natürliche Quelle.

## Wieder aktivieren

Falls der Builder in irgendeiner Form wiederbelebt wird, sind die
Reaktivierungsschritte minimal:

1. `oos-builder/` ist already in place — nichts zu tun.
2. In `ooso/go.mod`: `require onisin.com/oos-builder v0.0.0` plus
   `replace onisin.com/oos-builder => ../oos-builder`.
3. Den Visual-Knopf-Block im `ooso/gui/dsl_panel.go` wiederherstellen
   (siehe git history vor dem Rückbau-Commit).

Vor der Reaktivierung den Properties-Loop-Bug fixen (siehe oben).
