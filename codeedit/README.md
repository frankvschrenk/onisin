# fyne-codeedit

A lightweight code-editor widget for [Fyne](https://fyne.io) applications.

Not a replacement for Zed, VS Code or similar — it targets apps that need
to edit small amounts of structured text (XML, DSL snippets, TOML, config
files) with ergonomics that beat a plain `widget.Entry` without pulling in
a full editor stack.

## Install

```
go get github.com/frankvschrenk/fyne-codeedit
```

## Usage

```go
import codeedit "github.com/frankvschrenk/fyne-codeedit"

editor := codeedit.NewWithLanguage(codeedit.LangXML)
editor.SetText(myXML)

// Embed in your window like any other Fyne widget:
window.SetContent(container.NewBorder(nil, nil, nil, nil, editor))
```

## Shortcuts

| Key           | Action                                |
| ------------- | ------------------------------------- |
| `Tab`         | Insert indent unit (default 2 spaces) |
| `Shift+Tab`   | Dedent current line                   |
| `Enter`       | Newline with indent from current line |
| `Ctrl+D`      | Delete current line                   |
| `Ctrl+L`      | Insert empty line above               |
| `Ctrl+K`      | Toggle line comment                   |

> **Key choices:**
>
> - `Ctrl+L` instead of `Ctrl+I` for "insert line" — `Ctrl+I` is the
>   ASCII code for Tab and can be swallowed by the keyboard driver.
> - `Ctrl+K` instead of `Ctrl+/` for toggle-comment — `/` lives under a
>   modifier on many non-US keyboards (German: Shift+7), and Fyne
>   delivers the unmodified basekey when Ctrl is held, so `Ctrl+/`
>   arrives as `Ctrl+-` on DE layouts.

All other keys behave exactly like `widget.Entry` — copy, paste, undo,
selection, arrow navigation, etc. are inherited unchanged.

## Languages

Built-in comment-style presets via `NewWithLanguage`:

- `LangXML`  — `<!-- ... -->`
- `LangGo`   — `//`
- `LangTOML` — `#`
- `LangYAML` — `#`
- `LangSQL`  — `--`

For anything else, use `New()` and set `editor.Comment` by hand:

```go
editor := codeedit.New()
editor.Comment = codeedit.CommentStyle{LinePrefix: ";"}  // Lisp
```

## Demo

```
git clone https://github.com/frankvschrenk/fyne-codeedit
cd fyne-codeedit
go run ./cmd/demo
```

## Roadmap

- **v0.1** (this release) — Entry-based editor with editor shortcuts.
  No syntax highlighting.
- **v0.2** — Read-only highlighted view via
  [chroma](https://github.com/alecthomas/chroma), rendered as coloured
  `canvas.Text` segments. Useful for diff views, log output, and
  code previews.
- **v0.3** — Editable widget with live syntax highlighting. Requires
  a custom widget (cursor, selection, scroll) because `widget.Entry`
  does not expose per-token styling hooks.

## License

MIT — see [LICENSE](./LICENSE).
