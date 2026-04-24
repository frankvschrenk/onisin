package codeedit

import (
	"strings"

	"fyne.io/fyne/v2"
)

// handleCustomShortcut inspects a shortcut and runs the editor-local
// handler for it, returning true if the shortcut was consumed.
// Called from CodeEditor.TypedShortcut before the embedded Entry's
// TypedShortcut gets a chance — when the editor owns the keyboard,
// Fyne routes shortcuts to the focused widget first (see
// glfw/window.go ~ line 828), so this is the only place custom
// editor shortcuts can fire.
//
// Matching is done against KeyboardShortcut rather than a concrete
// type so it works for any shortcut wiring (desktop.CustomShortcut,
// future providers) without binding to implementation details.
func (e *CodeEditor) handleCustomShortcut(s fyne.Shortcut) bool {
	ks, ok := s.(fyne.KeyboardShortcut)
	if !ok {
		return false
	}
	key := ks.Key()
	mod := ks.Mod()

	// Primary modifier is platform-dependent: Cmd on macOS, Ctrl
	// elsewhere. Fyne normalises this to KeyModifierShortcutDefault.
	//
	// We match against both the "default" modifier and plain Ctrl so
	// users on either platform hit the same keys, and so CustomShortcut
	// bindings registered with Ctrl on macOS still work.
	isPrimary := mod&fyne.KeyModifierShortcutDefault != 0 ||
		mod&fyne.KeyModifierControl != 0
	hasShift := mod&fyne.KeyModifierShift != 0

	switch {
	case key == fyne.KeyTab && hasShift:
		e.dedentCurrentLine()
		return true
	case key == fyne.KeyD && isPrimary:
		e.deleteCurrentLine()
		return true
	case key == fyne.KeyL && isPrimary:
		// Ctrl+L — insert line above. Moved from Ctrl+I because
		// some terminals and GLFW drivers treat Ctrl+I as literal
		// Tab (ASCII 0x09), which swallows the shortcut before the
		// widget sees it.
		e.insertLineAbove()
		return true
	case key == fyne.KeyK && isPrimary:
		// Ctrl+K — toggle comment. Chosen over the more common
		// Ctrl+/ because "/" lives under a modifier on many
		// non-US keyboards (DE: Shift+7), and Fyne delivers the
		// unmodified basekey when Ctrl is held, so Ctrl+/ ends
		// up as Ctrl+- on German layouts.
		e.toggleComment()
		return true
	}
	return false
}

// ── Line operations ──────────────────────────────────────────────────

// dedentCurrentLine removes one IndentUnit from the start of the
// current line if present. Silent no-op if the line does not start
// with the configured indent string.
func (e *CodeEditor) dedentCurrentLine() {
	if e.IndentUnit == "" {
		return
	}
	lines := strings.Split(e.Text, "\n")
	row := e.CursorRow
	if row < 0 || row >= len(lines) {
		return
	}
	if !strings.HasPrefix(lines[row], e.IndentUnit) {
		return
	}
	lines[row] = lines[row][len(e.IndentUnit):]

	newCol := e.CursorColumn - len(e.IndentUnit)
	if newCol < 0 {
		newCol = 0
	}
	e.setTextKeepingCursor(strings.Join(lines, "\n"), row, newCol)
}

// deleteCurrentLine removes the line under the cursor. If it's the
// last line, leaves an empty string (not an out-of-range cursor).
func (e *CodeEditor) deleteCurrentLine() {
	lines := strings.Split(e.Text, "\n")
	row := e.CursorRow
	if row < 0 || row >= len(lines) {
		return
	}

	// Special case: a single line — clear its content but keep the
	// line itself, because Entry doesn't deal well with a zero-line
	// buffer.
	if len(lines) == 1 {
		e.setTextKeepingCursor("", 0, 0)
		return
	}

	lines = append(lines[:row], lines[row+1:]...)

	// Move the cursor onto what is now the same row (or the last
	// row if we just deleted the final line), at column 0.
	newRow := row
	if newRow >= len(lines) {
		newRow = len(lines) - 1
	}
	e.setTextKeepingCursor(strings.Join(lines, "\n"), newRow, 0)
}

// insertLineAbove inserts an empty line above the current one and
// places the cursor at its start. Matches the Ctrl+L / VS Code "insert
// line above" gesture.
func (e *CodeEditor) insertLineAbove() {
	lines := strings.Split(e.Text, "\n")
	row := e.CursorRow
	if row < 0 || row > len(lines) {
		return
	}

	// Preserve the indent of the current line so the new line is
	// ready to be typed into.
	var indent string
	if row < len(lines) {
		indent = leadingWhitespace(lines[row])
	}

	lines = append(lines[:row], append([]string{indent}, lines[row:]...)...)
	e.setTextKeepingCursor(strings.Join(lines, "\n"), row, len(indent))
}

// toggleComment wraps or unwraps the current line with the configured
// comment style. No-op if Comment is empty. The cursor column is kept
// stable relative to the non-comment content so repeated toggling
// doesn't drift the caret.
func (e *CodeEditor) toggleComment() {
	if e.Comment.LinePrefix == "" && e.Comment.BlockOpen == "" {
		return
	}
	lines := strings.Split(e.Text, "\n")
	row := e.CursorRow
	if row < 0 || row >= len(lines) {
		return
	}
	line := lines[row]
	indent := leadingWhitespace(line)
	body := line[len(indent):]

	var newBody string
	var cursorDelta int

	switch {
	case e.Comment.LinePrefix != "":
		prefix := e.Comment.LinePrefix + " "
		if strings.HasPrefix(body, prefix) {
			newBody = body[len(prefix):]
			cursorDelta = -len(prefix)
		} else if strings.HasPrefix(body, e.Comment.LinePrefix) {
			newBody = body[len(e.Comment.LinePrefix):]
			cursorDelta = -len(e.Comment.LinePrefix)
		} else {
			newBody = prefix + body
			cursorDelta = len(prefix)
		}
	case e.Comment.BlockOpen != "":
		open := e.Comment.BlockOpen + " "
		closeStr := " " + e.Comment.BlockClose
		if strings.HasPrefix(body, open) && strings.HasSuffix(body, closeStr) {
			newBody = body[len(open) : len(body)-len(closeStr)]
			cursorDelta = -len(open)
		} else {
			newBody = open + body + closeStr
			cursorDelta = len(open)
		}
	}

	lines[row] = indent + newBody

	newCol := e.CursorColumn + cursorDelta
	if newCol < len(indent) {
		newCol = len(indent)
	}
	if newCol > len(lines[row]) {
		newCol = len(lines[row])
	}
	e.setTextKeepingCursor(strings.Join(lines, "\n"), row, newCol)
}

// setTextKeepingCursor replaces the whole buffer and restores the
// cursor to (row, col). A single SetText call is cheaper than walking
// the buffer line by line, and Fyne's internal layout recompute handles
// the repaint for us.
func (e *CodeEditor) setTextKeepingCursor(text string, row, col int) {
	e.Entry.SetText(text)
	e.CursorRow = row
	e.CursorColumn = col
	e.Refresh()
}
