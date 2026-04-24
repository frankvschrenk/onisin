package codeedit

// highlight.go — read-only highlighted text view backed by chroma.
//
// This is the v0.2 read-only path of the roadmap: a widget that
// renders coloured syntax-highlighted code without any editing
// capability. It sidesteps the hard problems of a full editor
// (cursor, selection, per-token editing) while giving callers a
// way to show code snippets, diffs and log output in colour.
//
// Layout strategy:
//
//   - Chroma tokenises the source into a flat stream of Token values
//     (text + token type).
//   - We walk the stream line by line: whenever a token contains \n
//     we split it and start a new visual row.
//   - Each row is an HBox of coloured canvas.Text objects, one per
//     token (or token-fragment between newlines).
//   - All rows live in a VBox wrapped in a VScroll.
//
// Leading whitespace is preserved by substituting regular spaces
// with U+00A0 (non-breaking space) inside canvas.Text, because Fyne's
// default rasteriser collapses runs of regular spaces at line starts.

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// tightRowLayout is a minimal horizontal layout that places its
// children flush against each other with no inter-child padding.
//
// Fyne's default HBox inserts theme padding between every child,
// which is fine for buttons and labels but kills the illusion of
// continuous text when we put one canvas.Text per token next to each
// other: "name" + "=" + "\"x\"" gets rendered as 'name = "x"' with
// visible gaps. This layout places each child at the previous
// child's right edge, preserving the source's own spacing.
//
// Vertical alignment is "baseline-ish": all children start at y=0
// of the row, which is correct because every child is a canvas.Text
// with identical height at a shared TextSize.
type tightRowLayout struct{}

func (tightRowLayout) Layout(objects []fyne.CanvasObject, _ fyne.Size) {
	x := float32(0)
	for _, o := range objects {
		sz := o.MinSize()
		o.Move(fyne.NewPos(x, 0))
		o.Resize(sz)
		x += sz.Width
	}
}

func (tightRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		sz := o.MinSize()
		w += sz.Width
		if sz.Height > h {
			h = sz.Height
		}
	}
	return fyne.NewSize(w, h)
}

// Compile-time check: tightRowLayout must satisfy fyne.Layout.
var _ fyne.Layout = tightRowLayout{}

// tightColumnLayout stacks rows flush against each other with no
// inter-row padding. Used for the vertical assembly of code lines so
// the rendered output looks like a continuous block of source, not a
// padded list of widgets.
type tightColumnLayout struct{}

func (tightColumnLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		sz := o.MinSize()
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, sz.Height))
		y += sz.Height
	}
}

func (tightColumnLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		sz := o.MinSize()
		if sz.Width > w {
			w = sz.Width
		}
		h += sz.Height
	}
	return fyne.NewSize(w, h)
}

var _ fyne.Layout = tightColumnLayout{}

// HighlightedView returns a Fyne widget that renders source with
// syntax highlighting. The result is a scrollable, read-only widget
// ready to embed with container.NewBorder or similar.
//
// If the language is empty or not recognised by chroma, the text is
// rendered in the theme's foreground colour without colouring.
//
// Style name defaults to "github" when empty. Any chroma style
// registered via chroma/styles.Registry is valid — "monokai",
// "vs", "solarized-light", etc.
func HighlightedView(source, language, style string) fyne.CanvasObject {
	lexer := pickLexer(language)
	chromaStyle := pickStyle(style)

	iter, err := lexer.Tokenise(nil, source)
	if err != nil {
		// Tokeniser failures are very rare, but we still render
		// something useful rather than a blank pane.
		return scrollable(plainRow(source))
	}

	rows := renderRows(iter.Tokens(), chromaStyle)
	return scrollable(container.New(tightColumnLayout{}, rows...))
}

// ── Lexer / style resolution ──────────────────────────────────────────

// pickLexer resolves a language name to a chroma lexer. Unknown or
// empty names fall back to the "fallback" lexer which colours nothing,
// keeping the view useful for arbitrary plain text.
func pickLexer(language string) chroma.Lexer {
	if language == "" {
		return lexers.Fallback
	}
	if l := lexers.Get(language); l != nil {
		return l
	}
	return lexers.Fallback
}

// pickStyle resolves a style name the same way. "github" is a light
// theme that reads well against our default Fyne paper background;
// callers on dark Fyne themes will want "monokai" or "dracula".
func pickStyle(name string) *chroma.Style {
	if name == "" {
		name = "github"
	}
	if s := styles.Get(name); s != nil {
		return s
	}
	return styles.Fallback
}

// ── Row rendering ─────────────────────────────────────────────────────

// renderRows walks the token stream and builds one HBox per source
// line. A single chroma token may span multiple lines (pre-formatted
// whitespace, multi-line comments) so we split on '\n' and emit a
// fresh HBox after each newline.
func renderRows(tokens []chroma.Token, style *chroma.Style) []fyne.CanvasObject {
	var rows []fyne.CanvasObject
	var current []fyne.CanvasObject

	flush := func() {
		if len(current) == 0 {
			// An empty row still needs an object so VBox gives it
			// a line's worth of vertical space. An empty text
			// with a single non-breaking space is the cheapest
			// spacer that honours the theme text size.
			current = append(current, spacerText())
		}
		// tightRowLayout — no inter-child padding, so consecutive
		// chroma tokens render as one continuous line.
		rows = append(rows, container.New(tightRowLayout{}, current...))
		current = nil
	}

	for _, tok := range tokens {
		parts := strings.Split(tok.Value, "\n")
		for i, part := range parts {
			if part != "" {
				current = append(current, coloured(part, tok.Type, style))
			}
			// Every split except the last one represents a \n —
			// the content before the \n is part of the *current*
			// row, then we wrap the row.
			if i < len(parts)-1 {
				flush()
			}
		}
	}
	// Emit the trailing row even if it has no newline at the end.
	if len(current) > 0 {
		flush()
	}
	return rows
}

// coloured returns a canvas.Text for the given fragment, coloured
// according to the token type under the chosen style. Leading runs
// of spaces are converted to non-breaking spaces so Fyne does not
// collapse them at the start of a row.
func coloured(text string, tt chroma.TokenType, style *chroma.Style) *canvas.Text {
	entry := style.Get(tt)

	t := canvas.NewText(preserveLeadingSpaces(text), chromaColor(entry.Colour))
	t.TextStyle = fyne.TextStyle{
		Monospace: true,
		Bold:      entry.Bold == chroma.Yes,
		Italic:    entry.Italic == chroma.Yes,
	}
	return t
}

// spacerText returns a single non-breaking space used to give empty
// lines vertical height.
func spacerText() *canvas.Text {
	t := canvas.NewText("\u00A0", color.Transparent)
	t.TextStyle = fyne.TextStyle{Monospace: true}
	return t
}

// plainRow wraps plain source text (no lexing) in a single scrollable
// monospace block. Used as a fallback when tokenisation fails.
func plainRow(source string) fyne.CanvasObject {
	lines := strings.Split(source, "\n")
	rows := make([]fyne.CanvasObject, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			rows = append(rows, spacerText())
			continue
		}
		t := canvas.NewText(preserveLeadingSpaces(line), color.Black)
		t.TextStyle = fyne.TextStyle{Monospace: true}
		rows = append(rows, t)
	}
	return container.New(tightColumnLayout{}, rows...)
}

// scrollable wraps the view in a VScroll so long content is navigable.
func scrollable(inner fyne.CanvasObject) fyne.CanvasObject {
	return container.NewScroll(inner)
}

// preserveLeadingSpaces replaces leading runs of regular spaces with
// non-breaking spaces so Fyne's text layout does not collapse them.
// Tabs inside the source are expanded to two non-breaking spaces so
// indentation stays visible regardless of the source file's tab style.
//
// Only the leading whitespace is rewritten; embedded spaces are
// preserved verbatim because Fyne only collapses them at the start
// of a text object.
func preserveLeadingSpaces(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	i := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case ' ':
			b.WriteRune('\u00A0')
		case '\t':
			b.WriteString("\u00A0\u00A0")
		default:
			b.WriteString(s[i:])
			return b.String()
		}
	}
	return b.String()
}

// chromaColor converts a chroma.Colour to Fyne's image/color.Color.
// Chroma exposes RGB components directly — no alpha — so we force
// full opacity. A zero Colour value maps to the theme's default
// foreground by returning nil (which canvas.NewText treats as the
// theme foreground).
func chromaColor(c chroma.Colour) color.Color {
	if !c.IsSet() {
		return nil
	}
	return color.NRGBA{
		R: c.Red(),
		G: c.Green(),
		B: c.Blue(),
		A: 0xff,
	}
}
