# Filter Syntax

The list view's filter row, the GraphQL query interface, and the
LLM-driven natural-language filter all share the same wire
format: `<field>_<operator>:<value>`. A bare `<field>:<value>`
shortcut is treated as `<field>_eq:<value>`.

A field is filterable only when its domain marks it with the
`filterable` modifier — see [Domain DSL](domain.md#field).

## Operators

Eight operators are supported. Which ones apply depends on the
field's type.

| Operator | Meaning                       | Applies to                                      |
| -------- | ----------------------------- | ----------------------------------------------- |
| `_eq`    | equals                        | every type                                      |
| `_ne`    | not equals                    | every type                                      |
| `_lt`    | less than                     | `int`, `float`, `date`, `datetime`              |
| `_le`    | less than or equal            | `int`, `float`, `date`, `datetime`              |
| `_gt`    | greater than                  | `int`, `float`, `date`, `datetime`              |
| `_ge`    | greater than or equal         | `int`, `float`, `date`, `datetime`              |
| `_like` (also `_contains`) | substring (ILIKE) | `string`, `text`                              |
| `_in`    | one of (comma-separated list) | every type                                      |

The canonical source of truth is `operatorsForType()` in
`packages/oos-dsls-ts/src/parse.ts`. The GraphQL filter args are
generated from the same function — if the operator does not
match the field's type, the GraphQL schema will not even expose
the corresponding argument.

## Value formats

Values are unquoted on the wire and quoted in the DSL `example`
form. The renderer takes care of the boundary; the LLM sees both.

- **Strings**: any text. Operators that do substring matching
  (`_like`, `_contains`) are case-insensitive.
- **Numbers**: integer or decimal, no thousands separator.
- **Booleans**: `true` or `false`.
- **Dates**: ISO `YYYY-MM-DD`.
- **Datetimes**: ISO `YYYY-MM-DDThh:mm:ss[.sss]Z`.
- **`_in` lists**: comma-separated values. No spaces around the
  comma. Strings are not quoted: `roles_in:admin,manager`.

## Examples

```
lastname_contains:Cohen
age_ge:18
created_at_gt:2026-01-01
role_in:admin,manager
status:active                       # shortcut for status_eq:active
```

A bare value with no operator is `_eq` against the field. This
keeps the common case short — most user-facing filters check
equality.

## Author-supplied examples

A `field` block may carry one or more `example` declarations.
These never run; they exist only to give the LLM idiomatic
shapes to imitate when it translates natural language into
filter arguments.

```
field lastname : string filterable {
  example like "Cohen" "Last name contains 'Cohen'"
  example eq   "Smith" "Exact last name match"
}
```

The `example` operator names match the wire form without the
underscore prefix: `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `like`,
`in`. The literal can be a quoted string or a bare number; the
description is always a quoted string in the author's language
(German is fine — the LLM is multilingual).

## How filters travel through the stack

1. **Editor / list view** — the user types `Cohen` into the
   filter row's lastname column.
2. **Frontend** — the column emits `lastname_contains:Cohen`.
3. **oosgql** — `/query` parses the suffix, picks the operator
   from `operatorsForType()`, and builds an ILIKE clause.
4. **Postgres** — receives a parameterised query; user input
   never touches the SQL string.

## Common pitfalls

**Numeric `_like` does not exist.** `age_like:18` will not match
because `_like` only applies to string-shaped types. Use `_eq` or
`_ge`/`_le` instead.

**`_in` with a single value works.** It is a less efficient `_eq`,
but useful when the UI is built around a multi-select.

**Empty `_in` lists are an error.** If the multi-select is empty,
omit the filter entirely instead of sending `roles_in:`.

**`_like` is anchored automatically.** The operator wraps the
value in `%...%` server-side; do not include the wildcards
yourself.

## See also

- **[Domain DSL — fields](domain.md#field)** for marking a field
  filterable and writing examples.
- **[View DSL — tables](view.md#tables)** for where filters
  surface in the UI.
