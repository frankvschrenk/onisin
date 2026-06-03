# Help

Reference material for authoring `domain` and `view` sources in
the oosd editor. Every topic on the left is a Markdown document
that ships with the editor; new topics are dropped into
`oosd/docs/help/` and the next dev build picks them up.

- **[Quick Start](quickstart.md)** — a complete domain + list +
  detail view in under fifty lines, copy-paste ready.
- **[Domain DSL](domain.md)** — fields, types, modifiers,
  permissions, relations, metas, AI hints.
- **[View DSL](view.md)** — toolbar, body, containers, table,
  navigation actions.
- **[Widget Reference](widgets.md)** — every widget tag and which
  Mantine component it produces.
- **[Filter Syntax](filters.md)** — how `<field>_<op>:<value>`
  reaches the GraphQL backend, which operators apply to which type.
- **[Permissions](permissions.md)** — how `permission` blocks gate
  view actions and oosgql mutations.

If you find yourself guessing, that is a doc gap — write it down
and we will fill it in.
