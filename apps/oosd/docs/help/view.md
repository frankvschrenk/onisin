# View DSL

A `view` describes how a domain is shown to a human: the
top-level toolbar, the body, the widgets bound to fields, and the
navigation between views. Every view binds to exactly one domain
via `over <domain>`, and its widget bindings reference fields as
`<domain>.<field>`.

One view per file. The file extension is `.view`; rows are
stored in `oos.view` keyed by the view name.

## Header

```
view <name> "<title>" over <domain> {
  ...members
}
```

| Part         | Meaning                                                                                  |
| ------------ | ---------------------------------------------------------------------------------------- |
| `<name>`     | Identifier used by other views' navigation (`-> <name>`).                                |
| `"<title>"`  | Human-readable label shown in the window title.                                          |
| `<domain>`   | The single domain this view binds against. Must exist in `oos.domain`.                   |

A view body is built from **toolbar** items, **containers**,
**widgets**, **tables**, and **static decorations**.

## Toolbar

The toolbar is a single block at the top of the view body.

```
toolbar {
  save
  delete confirm="Really delete?"
  exit
  new "New Note" -> note_detail as tab
}
```

| Item              | Effect                                                                                       |
| ----------------- | -------------------------------------------------------------------------------------------- |
| `save`            | Persists the bound row through oosgql. Disabled when nothing is dirty.                       |
| `delete`          | Deletes the bound row. Optional `confirm="..."` prompts before firing.                       |
| `exit`            | Closes the view (or returns to the parent if presented as a tab).                            |
| `new "<caption>" -> <view> [navmod...]` | Opens a sibling view, typically for inserting a new row.            |

Navigation modifiers (also used by `on_select`/`on_new`/`on_edit` table actions and view-level transitions) are listed below.

## Body elements

Containers organise the layout; widgets bind to fields; tables
render lists of rows. Every container takes child elements in
braces.

| Tag         | Purpose                                                                                                              |
| ----------- | -------------------------------------------------------------------------------------------------------------------- |
| `section`   | Top-level grouping with an optional caption.                                                                         |
| `stack`     | Vertical run of children — the most common container.                                                                |
| `row`       | Horizontal run of children, wraps when narrow.                                                                       |
| `grid cols=<n>` | Equal-width columns.                                                                                              |
| `tabs`      | Each `tab "<caption>" { ... }` becomes a tab pane.                                                                   |
| `accordion` | Each `item "<caption>" [open] { ... }` becomes a collapsible item; `open` makes it expanded by default.              |
| `card`      | Bordered, padded block.                                                                                              |
| `divider`   | Thin horizontal line. No body.                                                                                       |
| `sep`       | Thin vertical line. No body.                                                                                         |
| `table -> <field-ref> { ... }` | Bound list — see [Tables](#tables).                                                              |

Containers and widgets accept layout modifiers — see [Layout
modifiers](#layout-modifiers).

## Widgets

```
<kind> [ "<caption>" ] -> <domain>.<field> [<modifier>...]
```

`<kind>` is one of the values listed in
[Widget Reference](widgets.md). Caption is optional but
recommended; if you skip it the renderer uses the field name.

Widget modifiers:

| Modifier                  | Effect                                                                              |
| ------------------------- | ----------------------------------------------------------------------------------- |
| `readonly`                | Widget renders disabled. Saves ignore changes.                                      |
| `focus`                   | Editor focuses this widget when the view first renders.                             |
| `expand`                  | Widget grows to fill its container's main axis.                                     |
| `placeholder = "<text>"`  | Placeholder text inside text-like widgets.                                          |
| `format = <token>`        | Format hint — see [Format tokens](#format-tokens).                                  |
| `min = <n>`               | Minimum value for numeric/range widgets.                                            |
| `max = <n>`               | Maximum value for numeric/range widgets.                                            |
| `step = <n>`              | Step size for numeric/slider widgets.                                               |
| `p=`/`m=`/`gap=`/`cols=`  | Standard layout modifiers — see below.                                              |

### Static widgets

These do not bind to a field; they decorate or trigger.

```
button "Save"          : save
link   "Docs" href="https://docs.onisin.com"
icon   "account-circle" size=20
richtext {
  heading "About"
  plain   "This view edits a single note."
}
```

`button`'s optional `: <action>` references a toolbar action by
name — `save`, `delete`, or a custom action you have wired up.
`richtext` accepts spans of style `plain`, `bold`, `italic`,
`heading`, `subheading`, `mono`.

## Tables

```
table -> <domain>.rows {
  <view-action>...
  column <field-ref> "<caption>" [width=<n>] [format=<token>]
  ...
}
```

`<domain>.rows` is the conventional row source. Within the table:

- **View actions** wire up row interactions.
- **Columns** declare what to display, in order.

### View actions

```
on_select -> <view> [navmod...]
on_new    -> <view> [navmod...]
on_edit   -> <view> [navmod...]
```

`on_select` fires when the user clicks a row. `on_new` and
`on_edit` are reserved for explicit Add/Edit affordances on the
table itself.

### Navigation modifiers

These appear after `-> <target_view>` in toolbar `new`, table
view actions, or future cross-view links.

| Modifier                       | Effect                                                                            |
| ------------------------------ | --------------------------------------------------------------------------------- |
| `bind = <domain>.<field>`      | Pass the value of this field as the new view's row id.                            |
| `as tab` / `as modal` / `as window` / `as replace` | Choose how the new view is presented.                          |
| `confirm = "<message>"`        | Ask the user before navigating.                                                   |

```
on_select -> note_detail bind=note.id as tab
new "New" -> note_detail as modal confirm="Discard your changes?"
```

## Layout modifiers

Containers and widgets accept the same set of layout modifiers,
all optional and chainable.

| Modifier                  | Effect                                                                                 |
| ------------------------- | -------------------------------------------------------------------------------------- |
| `p=<v>`, `pt=<v>`, `px=<v>`, … | Padding (token: `xs`/`sm`/`md`/`lg`/`xl`, or numeric scale).                       |
| `m=<v>`, `mt=<v>`, `mx=<v>`, … | Margin, same scale as padding.                                                     |
| `gap=<v>`                 | Spacing between children inside a `stack`/`row`/`grid`.                                |
| `cols=<n>`                | Column count on a `grid`.                                                              |
| `expand`                  | Container/widget grows to fill available space along its parent's main axis.           |
| `scroll = true`           | Wrap children in a scrollable region.                                                  |

```
section "Address" p=md gap=sm {
  text "Street" -> person.street expand
  row gap=sm {
    text "ZIP"  -> person.zip
    text "City" -> person.city expand
  }
}
```

## Format tokens

Format tokens narrow how a value is rendered.

| Token                            | Meaning                                                          |
| -------------------------------- | ---------------------------------------------------------------- |
| `currency`                       | Locale-aware currency for numeric fields.                        |
| `number:<n>`                     | Number with `<n>` decimal places.                                |
| `percent`                        | Render as `0.42 → 42%`.                                          |
| `date:short` / `date:medium` / `date:long` / `date:full` | Localised date style.                |
| `datetime:short` / `datetime:medium` / `datetime:long` / `datetime:full` | Localised date+time. |
| `time:short` / `time:medium`     | Localised time.                                                  |

```
text "Created" -> note.created_at readonly format=datetime:short
```

## Field references

A `<domain>.<field>` reference must point at a field of the
view's bound domain or one reached through a relation. The
parser allows any identifier as the field part, including names
that collide with widget keywords (e.g. a domain field called
`color` or `email`); the validator confirms the field actually
exists on the resolved domain.

## Lexical notes

- Identifiers: `[_a-zA-Z][\w_]*`
- Strings: double-quoted. `\"` escapes a quote inside a string.
- Comments: `// line` and `/* block */`.
- Whitespace is insignificant.

## See also

- **[Widget Reference](widgets.md)** — every binding widget and
  which Mantine component it produces.
- **[Domain DSL](domain.md)** — the field/permission/relation
  vocabulary the view binds against.
- **[Quick Start](quickstart.md)** — the smallest end-to-end
  example.
