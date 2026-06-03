# Naming — oosd, domain-dsl, view-dsl

Stand: 30. April 2026

Dieses Dokument hält fest, wie die Sprachen und das Tool um sie herum
heißen — und was die einzelnen Begriffe meinen. Es ersetzt die
heutigen Arbeitsnamen `ctx`, `dsl` und `ooso`.

## Drei Begriffe, drei Bedeutungen

### domain-dsl

Reines Datenmodell. Eine Domain beschreibt **was es gibt** — Felder mit
Typen, Permissions, Relationen, Lookup-Quellen, Filter-Hinweise und
freitextliche AI-Hints für die LLM. Eine Domain kennt keine UI: kein
Tab, kein Button, kein on_select.

Eine Domain pro fachlichem Konzept. Beispiel:

```
domain/person.domain
domain/note.domain
```

Datei-Endung: `.domain`

### view-dsl

Alles, was UI ist. Eine View beschreibt **wie etwas dem Menschen
gezeigt wird** — Layout, Widgets, Bindings, Navigation, Toolbar-Aktionen.
Views referenzieren Domains, aber Domains kennen keine Views.

Mehrere Views pro Domain sind die Norm: typischerweise eine Liste und
ein Detail. Beispiel:

```
view/person_list.view
view/person_detail.view
view/note_list.view
view/note_detail.view
```

Datei-Endung: `.view`

### oosd

Der Designer. Das Tool, in dem Domains und Views geschrieben, geprüft
und in die Datenbank gespeichert werden.

`oosd` ist nicht die Sprache. `oosd` ist der Editor.

## Warum diese Trennung?

Bisher (heute, in `oos.ctx`) waren Domain und View vermischt. Im
person-Context standen Felder *und* `<navigate event="on_select"
to="person_detail" .../>`. Das navigate ist eine UI-Entscheidung:
wenn der Mensch auf eine Tabellenzeile klickt, soll ein Detail-Tab
aufgehen. Diese Information gehört zur View, nicht zum Datenmodell.

Konsequente Trennung bringt drei Dinge:

1. **Eine Domain — viele Views.** Heute kann person nur ein
   on_select-Ziel haben. In der neuen Welt kann person_list-Standard
   `person_detail` öffnen, eine Quick-View `person_card` öffnen, eine
   spezialisierte HR-Sicht `person_hr_form` öffnen — alles aus
   derselben Domain.

2. **LLM-Fokus klar.** Wenn die LLM eine Filter-Frage beantworten
   soll, liest sie nur die Domain. Wenn sie die UI bedient, liest sie
   View + Domain. Heute mischt sich das zu einer großen Datei mit
   zwei Verantwortungen.

3. **Permissions am richtigen Ort.** Wer was lesen, schreiben oder
   löschen darf, ist eine Domain-Frage. Welcher Button das auslöst,
   eine View-Frage. Eine View darf nicht mehr erlauben als die Domain.

## Was wandert wohin?

| Heute (`ctx`)               | Neu                |
|-----------------------------|--------------------|
| `<field>` mit Typen         | domain-dsl         |
| `<permission>`              | domain-dsl         |
| `<relation>`                | domain-dsl         |
| `<meta>` (Lookup-Tabelle)   | domain-dsl         |
| `<example>` (Filter-Hint)   | domain-dsl         |
| `<ai>` (Free-form)          | domain-dsl         |
| `<list_fields>`             | view-dsl (Liste)   |
| `<navigate>` mit `to=...`   | view-dsl (Action)  |
| `<action type="delete">`    | view-dsl (Toolbar) |
| `<action type="save">`      | view-dsl (Toolbar) |

Alles aus der heutigen `oos.dsl` (Layout, Widgets) bleibt
selbstverständlich in der view-dsl, in neuer Schreibweise.

## Sprache vs. Speicherort

Die Sprachen sind abstrakt: keine Pixel, keine Mantine-Klassen, keine
React-Komponenten. Sie sprechen über Tabs, Sektionen, Felder,
Spacing-Tokens xs/sm/md/lg/xl. Ein Renderer setzt das in Mantine um;
ein anderer Renderer könnte es in Fyne oder DaisyUI umsetzen.

Speicherform in der Datenbank: noch offen. Heute liegen XML-Strings
in `oos.ctx` und `oos.dsl`. In Zukunft vermutlich der DSL-Quelltext
selbst (das ist die Quelle der Wahrheit) plus eine geparste
JSON-Repräsentation als Cache. Aber das ist ein späteres Thema.

## Begriffe, die wir nicht mehr verwenden

- `ctx` — verschwommen. Domain und Context werden in der Branche
  oft synonym verwendet, aber „Domain" ist verständlicher und
  präziser für das, was wir meinen.
- `dsl` als Datei-Endung — zu generisch. Beide neuen Sprachen sind
  DSLs. Endungen `.domain` und `.view` sagen sofort, was drin steht.
- `ooso` als Tool-Name — zu nah an `oos`/`oosp`. `oosd` als Designer
  trennt sich klar von der Runtime.

## Rechtliches

Der Designer und die Sprachen sind Teil des Onisin-OS-Projekts. BSL 1.1.
Copyright Frank & Tristan von Schrenk.
