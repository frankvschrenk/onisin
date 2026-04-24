// Package codeedit provides a lightweight code-editor widget for Fyne
// applications. It is not a replacement for Zed, VS Code or similar —
// it targets apps that need to edit small amounts of structured text
// (XML, DSL snippets, TOML, config files) with ergonomics that beat
// a plain [widget.Entry] without pulling in a full editor stack.
//
// The widget extends [widget.Entry] with:
//
//   - Tab and Shift+Tab for indent / dedent (configurable width)
//   - Enter auto-indents to match the current line
//   - Ctrl+D deletes the current line
//   - Ctrl+L inserts an empty line above the current one
//     (Ctrl+I is ASCII for Tab and can collide with Tab handling)
//   - Ctrl+K toggles a line comment (language-aware)
//     (Ctrl+/ would be nicer but maps to Ctrl+- on German keyboards)
//
// Syntax highlighting is deliberately out of scope for the base
// widget; highlight support is planned as a separate package built
// on top of [github.com/alecthomas/chroma] (see the README for the
// roadmap).
package codeedit

import (
	"fmt"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// debugEnabled turns on verbose logging of every incoming TypedKey
// and TypedShortcut call. Enable via CODEEDIT_DEBUG=1 in the
// environment. Off by default to keep production quiet.
var debugEnabled = os.Getenv("CODEEDIT_DEBUG") != ""

func debugf(format string, args ...any) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "[codeedit] "+format+"\n", args...)
	}
}

// CommentStyle describes how a language wraps or prefixes a commented
// line. Exactly one of LinePrefix or (BlockOpen, BlockClose) should be
// set for a given language. Empty values disable comment toggling.
type CommentStyle struct {
	// LinePrefix is the marker placed at the start of a commented
	// line (e.g. "//" for Go, "#" for TOML).
	LinePrefix string

	// BlockOpen / BlockClose wrap a commented line when the language
	// has no line-comment syntax (e.g. "<!--" / "-->" for XML).
	BlockOpen  string
	BlockClose string
}

// Language names recognised by the default comment-style lookup.
const (
	LangXML  = "xml"
	LangGo   = "go"
	LangTOML = "toml"
	LangYAML = "yaml"
	LangSQL  = "sql"
)

// defaultStyles maps the built-in language names to a sensible
// comment style. Applications may ignore this map entirely and set
// CommentStyle by hand on the editor.
var defaultStyles = map[string]CommentStyle{
	LangXML:  {BlockOpen: "<!--", BlockClose: "-->"},
	LangGo:   {LinePrefix: "//"},
	LangTOML: {LinePrefix: "#"},
	LangYAML: {LinePrefix: "#"},
	LangSQL:  {LinePrefix: "--"},
}

// CodeEditor is a multi-line text entry widget with editor-style
// keyboard shortcuts. The zero value is not usable — always construct
// via [New] or [NewWithLanguage].
type CodeEditor struct {
	widget.Entry

	// IndentUnit is the string inserted on Tab and stripped on
	// Shift+Tab. Typical values are "\t", "  " or "    ". Defaults
	// to two spaces when [New] or [NewWithLanguage] is used.
	IndentUnit string

	// Comment drives Ctrl+K toggling. Leave zero to disable.
	Comment CommentStyle

	// shiftDown tracks whether either Shift key is held. Fyne
	// delivers Shift and Tab as two separate TypedKey events rather
	// than bundling them into a modifier-aware shortcut, so we
	// need our own state to distinguish Tab from Shift+Tab.
	shiftDown bool
}

// New returns a CodeEditor with sensible defaults: multi-line, word
// wrap off, monospace-friendly tab width of two spaces, no comment
// style. Callers typically set Comment or IndentUnit after construction,
// or use [NewWithLanguage] to get a pre-configured editor for a known
// language.
func New() *CodeEditor {
	e := &CodeEditor{IndentUnit: "  "}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapOff
	e.ExtendBaseWidget(e)
	return e
}

// NewWithLanguage returns a CodeEditor pre-configured for one of the
// built-in languages (see the Lang* constants). Unknown language names
// are treated as "no comment style configured" and otherwise behave
// like [New].
func NewWithLanguage(lang string) *CodeEditor {
	e := New()
	if style, ok := defaultStyles[lang]; ok {
		e.Comment = style
	}
	return e
}

// TypedKey intercepts Tab and Return so we can implement indent and
// auto-indent without relying on shortcuts (both keys arrive as
// plain TypedKey events, not as TypedShortcut).
//
// Shift+Tab is detected here because Fyne delivers Shift and Tab as
// separate TypedKey events; we track Shift state in KeyDown/KeyUp
// below and look it up when Tab arrives.
func (e *CodeEditor) TypedKey(ev *fyne.KeyEvent) {
	debugf("TypedKey name=%q physical=%v shift=%v", ev.Name, ev.Physical, e.shiftDown)
	switch ev.Name {
	case fyne.KeyTab:
		if e.shiftDown {
			e.dedentCurrentLine()
		} else {
			e.insertAtCursor(e.IndentUnit)
		}
		return
	case fyne.KeyReturn, fyne.KeyEnter:
		e.handleEnter()
		return
	}
	e.Entry.TypedKey(ev)
}

// KeyDown tracks modifier state so TypedKey can tell Tab from
// Shift+Tab. Implementing this also makes the widget satisfy
// desktop.Keyable, which is how Fyne recognises that we want raw
// key events in addition to the higher-level TypedKey callback.
func (e *CodeEditor) KeyDown(ev *fyne.KeyEvent) {
	switch ev.Name {
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		e.shiftDown = true
	}
}

// KeyUp mirrors KeyDown and clears the tracked modifier state.
func (e *CodeEditor) KeyUp(ev *fyne.KeyEvent) {
	switch ev.Name {
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		e.shiftDown = false
	}
}

// TypedShortcut intercepts our editor-local shortcuts (dedent,
// delete line, insert line, toggle comment) and forwards anything
// else to the embedded Entry so copy/paste/undo/select-all keep
// working unchanged.
//
// We dispatch here rather than via canvas.AddShortcut because Fyne's
// window loop routes keyboard shortcuts to the focused widget first,
// so canvas-level bindings never fire while the editor has the
// keyboard (see glfw/window.go in fyne v2).
func (e *CodeEditor) TypedShortcut(s fyne.Shortcut) {
	if ks, ok := s.(fyne.KeyboardShortcut); ok {
		debugf("TypedShortcut name=%q key=%q mod=0b%b",
			s.ShortcutName(), ks.Key(), ks.Mod())
	} else {
		debugf("TypedShortcut name=%q (non-keyboard)", s.ShortcutName())
	}
	if e.handleCustomShortcut(s) {
		return
	}
	e.Entry.TypedShortcut(s)
}

// SetText overrides Entry.SetText so callers can stay type-safe against
// the editor without having to reach through to the embedded Entry
// explicitly. Fyne's composition-by-embedding works either way, but a
// direct method makes call sites read better.
func (e *CodeEditor) SetText(s string) {
	e.Entry.SetText(s)
}

// ── internal helpers ─────────────────────────────────────────────────

// insertAtCursor inserts s at the current cursor position. Selection
// (if any) is replaced. Delegates to Entry.TypedRune for each rune so
// the edit history stays consistent with regular typing.
func (e *CodeEditor) insertAtCursor(s string) {
	for _, r := range s {
		e.Entry.TypedRune(r)
	}
}

// handleEnter inserts a newline and re-emits the leading whitespace
// of the current line so the caret lands under the first non-blank
// character of the line we just left.
func (e *CodeEditor) handleEnter() {
	currentLine := e.lineText(e.CursorRow)
	indent := leadingWhitespace(currentLine)

	e.insertAtCursor("\n")
	if indent != "" {
		e.insertAtCursor(indent)
	}
}

// lineText returns the text of the given row, or empty string if the
// row is out of range. Works off Entry.Text to stay independent of
// Fyne's internal row cache.
func (e *CodeEditor) lineText(row int) string {
	lines := strings.Split(e.Text, "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// leadingWhitespace returns the run of space and tab characters at the
// start of s. Newlines are not treated as whitespace because s is
// expected to be a single line.
func leadingWhitespace(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return s
}
