# Quick Start

A working `domain` plus a list view and a detail view in under
fifty lines. Paste each block into the matching tab in the
editor's sidebar (`Domain` for the first, `View` for the rest)
and save. The Preview tab on the detail view shows mocked data
immediately.

## 1. Domain — `note`

```
domain note from note@demo {

  permission admin   read, write, delete
  permission manager read, write
  permission user    read

  field id         : int      readonly
  field person_id  : int      readonly filterable
  field title      : string   filterable {
    example like "Meeting" "Title contains 'Meeting'"
  }
  field body       : text
  field created_at : datetime readonly

  ai "scope"
     "Notes always belong to a person. Filter by person_id when
      retrieving a person's notes."
}
```

What it says, in one breath: there is a table called `note` in
the `demo` datasource; admins can do everything, users can only
read; the row has five fields with sensible read/write defaults;
`title` is filterable and has one author-supplied example for
the LLM that explains how a `like` filter on it reads.

## 2. List view — `note_list`

```
view note_list "Notes" over note {

  toolbar {
    new "New" -> note_detail as tab
  }

  table -> note.rows {
    on_select -> note_detail bind=note.id as tab

    column note.id         "ID"    width=60
    column note.title      "Title" width=300
    column note.created_at "Date"  width=160 format=datetime:short
  }
}
```

The view is `over note`, so `note.rows` is a valid table source.
The toolbar gets one button: **New**, which opens `note_detail`
in a new tab. Selecting a row in the table opens the same detail
view, this time bound to the clicked row's id.

## 3. Detail view — `note_detail`

```
view note_detail "Note — detail" over note {

  toolbar {
    save
    delete confirm="Really delete this note?"
    exit
  }

  section "Note" p=md {
    text "ID"        -> note.id         readonly
    text "Person"    -> note.person_id  readonly
    text "Date"      -> note.created_at readonly format=datetime:short
  }
  section "Content" p=md pt=0 {
    text     "Title" -> note.title focus
    textarea         -> note.body  placeholder="Note body..." p=sm mt=sm
  }
}
```

Two `section`s, six widgets, three toolbar items. The `focus`
modifier on `title` makes the cursor land there when the view
opens. `delete` carries an inline confirmation prompt.

## What the editor does for you

- **Source tab** — Monaco with syntax highlighting for both DSLs
  and live diagnostics from the Langium parser. Errors and
  warnings appear as red/orange underlines.
- **Preview tab** — for views, renders the layout with
  auto-generated mock data drawn from the bound domain. New views
  show populated forms before any database row exists.
- **Help tab** — this panel. Click `Widget Reference` first when
  you are deciding which input to use.

## Where to next

- **[Domain DSL](domain.md)** for the full field/permission/
  relation/meta vocabulary.
- **[Widget Reference](widgets.md)** when picking widgets for the
  detail view.
- **[Filter Syntax](filters.md)** when wiring the list view's
  filter row.
