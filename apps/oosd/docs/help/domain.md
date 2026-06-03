# Domain DSL

A `domain` describes the data model behind a screen: which table
it draws from, which fields exist, who is allowed to read or
modify them, how it relates to other domains, where dropdowns
get their options, and what the LLM should know about it.

UI behaviour belongs in the [view DSL](view.md). A domain never
mentions widgets, layout, or rendering.

One domain per file. The file extension is `.domain`; rows are
stored in `oos.domain` keyed by the domain name.

## Header

```
domain <name> from <table>@<dsn> {
  ...members
}
```

| Part      | Meaning                                                                                         |
| --------- | ----------------------------------------------------------------------------------------------- |
| `<name>`  | Identifier used everywhere downstream — view bindings, GraphQL types, embedded chunks.          |
| `<table>` | Underlying SQL table name. Usually identical to the domain name, but may differ.                |
| `<dsn>`   | Datasource registered in oosp. The default is `demo` for the bundled demo database.             |

`<name>` must not start with `global.` — that prefix is reserved
for embedded LLM prompts. The grammar's identifier rule already
disallows the dot, and a custom validator catches it as well in
case the grammar ever accepts dotted names.

## Members

Five member kinds may appear inside the body, in any order:

- **Permissions** — who may do what.
- **Fields** — the data shape, with optional examples.
- **Relations** — pointers to other domains.
- **Metas** — lookup tables for dropdown widgets.
- **AI hints** — free-form notes for the LLM.

### Permission

```
permission <role> <action> [, <action>]*
```

Actions are `read`, `write`, and `delete`. List one role per
line. Roles are arbitrary strings; the matrix is consulted by the
oosgql server on every mutation and can be extended without
schema changes.

```
permission admin   read, write, delete
permission manager read, write
permission user    read
```

### Field

```
field <name> : <type> [<modifier>...] [{ <example>... }]
```

Field types:

| Type       | SQL                       | GraphQL  |
| ---------- | ------------------------- | -------- |
| `int`      | `integer`                 | `Int`    |
| `float`    | `numeric` / `double`      | `Float`  |
| `string`   | `varchar`                 | `String` |
| `text`     | `text` (long-form)        | `String` |
| `bool`     | `boolean`                 | `Boolean`|
| `date`     | `date`                    | `String` (ISO) |
| `datetime` | `timestamp`               | `String` (ISO) |

Modifiers:

| Modifier              | Effect                                                                                  |
| --------------------- | --------------------------------------------------------------------------------------- |
| `readonly`            | View widgets bound to this field render disabled. Mutations ignore changes to it.       |
| `filterable`          | Field appears in GraphQL filter args and in the list-view filter row.                   |
| `options = <meta>`    | Field draws its allowed values from a [Meta](#meta) — the dropdown source.              |

#### Examples

Examples are author-supplied filter cases, written purely for the
LLM that translates natural-language queries into GraphQL filters.
They do not change runtime behaviour.

```
field lastname : string filterable {
  example like "Anna" "Last name contains 'Anna'"
  example eq   "Cohen" "Exact last name match"
}
```

Operators: `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `like`, `in`. See
[Filter Syntax](filters.md) for which ones apply to which type.

The literal value can be a string (in quotes) or a number; the
description is always a string. Both pieces are embedded into
the LLM chunk for this domain.

### Relation

```
relation <name> : <kind> <target> bind = <local_field> -> <foreign_field>
```

Kinds:

- `has_many`   — this row owns many rows in the target.
- `has_one`    — this row owns at most one row in the target.
- `belongs_to` — this row points up to one parent row.

```
relation notes : has_many   note   bind = id         -> person_id
relation owner : belongs_to person bind = person_id  -> id
```

Relations matter for two things: the GraphQL schema (a `notes`
field on `person`) and the LLM-side schema chunk (so the model
can answer "give me Anna's notes" by joining).

### Meta

```
meta <name> from <table> <value_field> <label_field>
     [order_by <field>] [via <dsn>]
```

A meta is a lookup source for dropdown widgets. The view widget
references the meta name; the resolver materialises it as
`{ value, label }` rows so the renderer always sees the same
shape regardless of source-column names.

```
meta roles       from oos_role        id name order_by name
meta departments from oos_department  code label
```

Inside a domain field:

```
field role_id : int filterable options = roles
```

`via <dsn>` lets a meta read from a different datasource than
its parent domain — useful when reference data lives in a
shared catalogue.

### AI hint

```
ai "<topic>" "<body>"
```

A free-form note for the LLM. Topic is short (a slug-like word);
body is one or more sentences. Hints are appended to the embedded
schema chunk and surface during retrieval.

```
ai "scope"
   "Notes always belong to a person. Filter by person_id when
    listing a person's notes."

ai "edit_behavior"
   "Only title and body are user-editable; everything else is
    system-managed."
```

Hints should describe **what the LLM cannot see from the schema
itself** — domain conventions, edit constraints, business rules.
Avoid restating field types or filter operators; the renderer
already derives those.

## Lexical notes

- Identifiers: `[_a-zA-Z][\w_]*`
- Strings: double-quoted. `\"` escapes a quote inside a string.
- Comments: `// line` and `/* block */`.
- Whitespace is insignificant.

## See also

- **[Filter Syntax](filters.md)** — how `field_op:value` reaches
  the database.
- **[Permissions](permissions.md)** — how the `permission` block
  is enforced at view time and at mutation time.
- **[Quick Start](quickstart.md)** — the smallest end-to-end
  example.
