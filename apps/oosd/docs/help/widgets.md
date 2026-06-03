# Widget Reference

Every binding widget tag in a `view` block maps to one Mantine
component. This page tells you which Mantine component you get,
what it looks like, and when to reach for it instead of a
neighbour. Container tags are listed at the bottom.

A common authoring mistake is using a read-only widget where an
editable one is meant — `progress` shows a value, it does not let
the user change it. Use `slider` or `rating` for editable values.

---

## Bindable widgets

These take a `bind: <domain>.<field>` and render an editable (or
read-only-by-modifier) input over that field.

### Text input

| Tag         | Mantine component                                                              | Use when                                                            |
| ----------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| `text`      | [TextInput](https://mantine.dev/core/text-input/)                              | Single-line strings — names, titles, identifiers.                   |
| `textarea`  | [Textarea](https://mantine.dev/core/textarea/)                                 | Multi-line free text — notes, descriptions, comments.               |
| `password`  | [PasswordInput](https://mantine.dev/core/password-input/)                      | Secrets the user types. Hides characters; offers reveal toggle.     |
| `email`     | [TextInput](https://mantine.dev/core/text-input/) (`type=email`)               | Email addresses. Browser keyboard hint on mobile; no extra widget.  |

### Numbers

| Tag           | Mantine component                                                            | Use when                                                          |
| ------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| `number`      | [NumberInput](https://mantine.dev/core/number-input/)                        | Any numeric value — counts, prices, ages. Honours `format`.       |
| `slider`      | [Slider](https://mantine.dev/core/slider/)                                   | **Editable** numeric value on a bounded range. Honours `min`/`max`/`step`. |
| `rangeslider` | [RangeSlider](https://mantine.dev/core/range-slider/)                        | Two-handled range — "from … to …" filtering or selection.         |
| `rating`      | [Rating](https://mantine.dev/core/rating/)                                   | Editable star rating, typically 1–5. Use instead of `slider` when the metaphor is qualitative. |
| `progress`    | [Progress](https://mantine.dev/core/progress/)                               | **Read-only.** Show how far along something is. Not for editing — use `slider` or `rating` instead. |

### Date & time

| Tag         | Mantine component                                                              | Use when                                                              |
| ----------- | ------------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| `date`      | [DateInput](https://mantine.dev/dates/date-input/) (`@mantine/dates`)          | Single calendar date. Honours `format` (date style).                  |
| `daterange` | [DatePickerInput type="range"](https://mantine.dev/dates/date-picker-input/)   | Two-date range — "from … to …". Single popover, two highlighted days. |
| `time`      | [TimeInput](https://mantine.dev/dates/time-input/)                             | Time of day. Renders as `hh:mm` (or `hh:mm:ss`).                      |

### Choice

| Tag           | Mantine component                                                            | Use when                                                                                                  |
| ------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `select`      | [Select](https://mantine.dev/core/select/)                                   | One value from a known list. The dropdown closes after pick. Use for `optionsRef` fields.                 |
| `multiselect` | [MultiSelect](https://mantine.dev/core/multi-select/)                        | Several values from a list. Tags appear inline. Use for tags, roles, comma-separated columns.             |
| `combobox`    | [Autocomplete](https://mantine.dev/core/autocomplete/)                       | One value, but typing narrows the list. Use when the list is long enough that scrolling is painful.       |
| `radio`       | [Radio.Group](https://mantine.dev/core/radio/)                               | One of a small (2–5) set, all visible at once. Use when seeing the alternatives matters more than space.  |
| `check`       | [Checkbox](https://mantine.dev/core/checkbox/)                               | Single boolean — "Active", "Subscribed". Label sits next to a square. **Not** `checkbox`.                 |

```
check "Aktiv" -> person.active
```
| `switch`      | [Switch](https://mantine.dev/core/switch/)                                   | Single boolean expressing **on/off state**, not agreement. "Notifications on", "Dark mode on".            |

### Files & color

| Tag     | Mantine component                                                              | Use when                                                              |
| ------- | ------------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| `file`  | [FileInput](https://mantine.dev/core/file-input/)                              | Pick a file from disk. Stores the file reference, not its contents.   |
| `color` | [ColorInput](https://mantine.dev/core/color-input/)                            | Pick a colour. Stores a CSS-style colour string.                      |

### Display-only

These render a value but do not edit it. They are appropriate as
read-only badges, avatars, or progress indicators inside a card or
table cell.

| Tag       | Mantine component                                                              | Use when                                                            |
| --------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| `progress`| [Progress](https://mantine.dev/core/progress/)                                 | Show a 0–100 fill bar. **Not editable.**                            |
| `badge`   | [Badge](https://mantine.dev/core/badge/)                                       | Compact status label — "active", "pending", "archived".             |
| `avatar`  | [Avatar](https://mantine.dev/core/avatar/)                                     | Profile picture or initials.                                        |

---

## Static widgets

These don't bind to a field. They sit in the layout to label, link,
or trigger actions.

| Tag        | Mantine component                                                              | Use when                                                            |
| ---------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| `button`   | [Button](https://mantine.dev/core/button/)                                     | Trigger a toolbar action (`actionRef`) or navigate.                 |
| `link`     | [Anchor](https://mantine.dev/core/anchor/)                                     | Inline hyperlink to an external resource.                           |
| `icon`     | [Tabler icon](https://tabler.io/icons) (lookup by name)                        | Decorative or semantic glyph in a row, button, or section header.   |
| `richtext` | [Text](https://mantine.dev/core/text/) / [Title](https://mantine.dev/core/title/) | Static paragraphs, headings, and inline emphasis. Use spans of `plain`, `bold`, `italic`, `heading`, `subheading`, `mono`. **No field bindings inside richtext.** |

```
richtext {
  heading "Schadensvorgang"
  bold    "Bitte alle Felder ausfüllen"
}
```

⚠️ **Wrong** — widget bindings are NOT allowed inside `richtext`:
```
richtext {
  text "Name" -> person.name   ← ERROR
}
```

---

## Containers

Containers organise the form. They take child elements but no
binding. All containers accept layout modifiers (`p`, `m`, `gap`,
`cols`, `expand`, `scroll`).

| Tag         | Mantine component                                                            | Use when                                                                                                  |
| ----------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `section`   | [Paper](https://mantine.dev/core/paper/) + [Title](https://mantine.dev/core/title/) | Top-level grouping with an optional caption. Vertical stack inside.                                       |
| `stack`     | [Stack](https://mantine.dev/core/stack/)                                     | Children stacked vertically. The default arrangement; reach for it first.                                 |
| `row`       | [Group](https://mantine.dev/core/group/)                                     | Children laid out horizontally, wrapping if needed. Use for two or three fields side by side.             |
| `grid`      | [SimpleGrid](https://mantine.dev/core/simple-grid/)                          | Equal-width columns. Set `cols` (e.g. `grid cols=3`).                                                     |
| `tabs`      | [Tabs](https://mantine.dev/core/tabs/)                                       | Hide alternative panes behind labels. Each `tab` has a caption + body.                                    |
| `accordion` | [Accordion](https://mantine.dev/core/accordion/)                             | Collapsible sections. Each item has a caption, an `open` flag, and a body.                                |
| `card`      | [Card](https://mantine.dev/core/card/)                                       | A bordered, padded block — visually distinct from its surroundings. Often used inside a `grid`.           |
| `divider`   | [Divider](https://mantine.dev/core/divider/) (horizontal)                    | A thin horizontal line. Use to separate sections inside a `stack`.                                        |
| `separator` | [Divider](https://mantine.dev/core/divider/) (vertical)                      | A thin vertical line. Use to separate items inside a `row`.                                               |
| `table`     | [Table](https://mantine.dev/core/table/)                                     | Render a list of records with a fixed column set. The body wraps a row source from a `has_many` relation. |

---

## Layout modifiers

These can appear after most container and widget tags to tweak
spacing or growth.

| Modifier                    | Effect                                                                                  |
| --------------------------- | --------------------------------------------------------------------------------------- |
| `p:md`, `pt:lg`, `px:sm`, … | Padding (token: `xs` / `sm` / `md` / `lg` / `xl`, or numeric scale).                    |
| `m:md`, `mt:lg`, `mx:sm`, … | Margin, same scale as padding.                                                          |
| `gap:md`                    | Spacing between children inside a stack/row/grid.                                       |
| `cols:3`                    | Column count on a `grid`.                                                               |
| `expand`                    | The element should grow to fill available space along its container's main axis.        |
| `scroll`                    | Wrap the children in a scrollable region.                                               |

---

## Common pitfalls

**`progress` for an editable value.** Mantine's `Progress` is a
status bar — read-only by design. If the user is supposed to drag
to set "profile completeness" or similar, switch to `slider`
(numeric) or `rating` (qualitative).

**`switch` vs. `check`.** Both produce a boolean, but the
metaphors differ. `switch` reads as a state toggle ("Notifications
on"); `check` reads as agreement or selection ("I accept the
terms"). Pick the one whose label sounds natural.

**`select` vs. `combobox` vs. `radio`.** Three ways to pick one
from a list. Use `radio` for very short lists where seeing all
options matters. Use `select` for medium lists. Use `combobox`
when the user types to find — typically dozens or hundreds of
options.

**`row` vs. `grid`.** `row` packs children with their natural
widths; columns can be uneven and wrap. `grid` enforces equal
widths via `cols`. For a form with two label-input pairs side by
side, `row` is usually right; for a four-card dashboard, `grid` is
right.
