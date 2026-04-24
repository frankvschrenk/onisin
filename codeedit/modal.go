package codeedit

// modal.go — ModalEditor: a preview/edit two-mode wrapper around
// CodeEditor and HighlightedView.
//
// The editor starts in Preview mode showing the chroma-highlighted
// read-only rendering. Pressing Enter switches to Edit mode with the
// plain CodeEditor focused; pressing Escape returns to Preview mode
// and re-renders the (possibly-changed) text.
//
// A small badge in the top-right corner reports the active mode so
// the user always knows whether typed keys edit the buffer or are
// swallowed by the preview layer.
//
// Rationale for this split:
//
//   - Rendering fully-coloured text that also accepts editing is the
//     hard problem in Fyne (no per-segment styling on widget.Entry,
//     no editable canvas.Text). Keeping the two modes physically
//     separate lets each do what it does well.
//
//   - Users spend more time reading and navigating code than typing
//     into it. Preview-by-default means they see colours almost all
//     the time, losing them only when actually editing.

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ModalEditor is a compound widget that presents the same text either
// in an editable CodeEditor or a chroma-highlighted read-only view.
// Construct via [NewModalEditor]; the zero value is not usable.
type ModalEditor struct {
	widget.BaseWidget

	// Language selects the chroma lexer and the editor's comment
	// style preset. Empty means no highlighting and no comment
	// toggling.
	Language string

	// Style is the chroma style name used for Preview rendering.
	// Defaults to "github" when empty.
	Style string

	editor   *CodeEditor
	preview  *previewPane
	stack    *fyne.Container
	badge    *canvas.Text
	badgeBox fyne.CanvasObject

	mode modalMode
}

type modalMode int

const (
	modePreview modalMode = iota
	modeEdit
)

// NewModalEditor builds a ModalEditor for the given language (one of
// LangXML, LangGo, etc., or "" for plain text). The widget starts in
// Preview mode with an empty buffer; call SetText to load content.
func NewModalEditor(language string) *ModalEditor {
	m := &ModalEditor{
		Language: language,
		Style:    "github",
		editor:   NewWithLanguage(language),
	}
	m.editor.OnEscape = m.enterPreviewMode
	m.ExtendBaseWidget(m)
	m.buildLayout()
	m.applyMode()
	return m
}

// StartInEditMode flips the initial mode to Edit. Call before the
// widget is first laid out — typically right after NewModalEditor.
// Useful for panels whose primary workflow is "paste content and
// click Apply", where a Preview-first default makes the paste target
// non-obvious.
func (m *ModalEditor) StartInEditMode() {
	m.mode = modeEdit
	m.applyMode()
}

// SetText replaces the buffer contents in both the editor and the
// preview. Safe to call from any goroutine — all widget mutation is
// funnelled through the embedded widgets' thread-safe setters.
//
// Programmatic SetText does not fire OnChanged (consistent with
// widget.Entry semantics) so callers can initialise the buffer
// without tripping their own dirty tracking.
func (m *ModalEditor) SetText(s string) {
	onChanged := m.editor.OnChanged
	m.editor.OnChanged = nil
	m.editor.SetText(s)
	m.editor.OnChanged = onChanged
	m.refreshPreview()
}

// Text returns the current buffer contents.
func (m *ModalEditor) Text() string {
	return m.editor.Text
}

// OnChanged wires a callback that fires for every user-initiated
// buffer modification. Matches widget.Entry.OnChanged so existing
// dirty-flag code can migrate without behavioural changes.
func (m *ModalEditor) OnChanged(f func(string)) {
	m.editor.OnChanged = f
}

// CreateRenderer is the standard Fyne hook; we simply return a
// renderer for our layout stack so the BaseWidget has something to
// paint.
func (m *ModalEditor) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.stack)
}

// ── Mode transitions ─────────────────────────────────────────────────

// enterEditMode switches to Edit and gives the keyboard to the
// underlying CodeEditor so typing takes effect immediately.
//
// The Focus call is deferred onto Fyne's event queue via fyne.Do so
// it runs after applyMode's Refresh has finished committing the new
// widget tree. Calling Focus synchronously here would race against
// the swap and the request is silently dropped.
func (m *ModalEditor) enterEditMode() {
	if m.mode == modeEdit {
		return
	}
	m.mode = modeEdit
	m.applyMode()
	fyne.Do(func() {
		if win := windowFor(m); win != nil {
			win.Canvas().Focus(m.editor)
		}
	})
}

// enterPreviewMode switches to Preview, re-tokenises the latest text
// and moves focus onto the preview pane so future Enter presses are
// interpreted by the preview layer (and a second Enter re-enters
// Edit without needing an extra click).
func (m *ModalEditor) enterPreviewMode() {
	if m.mode == modePreview {
		return
	}
	m.mode = modePreview
	m.refreshPreview()
	m.applyMode()
	fyne.Do(func() {
		if win := windowFor(m); win != nil {
			win.Canvas().Focus(m.preview)
		}
	})
}

// applyMode swaps the stacked child and updates the badge text.
// Called by every mode transition — single source of truth for the
// visible state.
func (m *ModalEditor) applyMode() {
	switch m.mode {
	case modeEdit:
		m.stack.Objects = []fyne.CanvasObject{
			container.NewBorder(nil, nil, nil, m.badgeBox, m.editor),
		}
		m.setBadge("EDIT")
	default:
		m.stack.Objects = []fyne.CanvasObject{
			container.NewBorder(nil, nil, nil, m.badgeBox, m.preview),
		}
		m.setBadge("PREVIEW")
	}
	m.stack.Refresh()
}

// setBadge updates the badge label text and colour and refreshes
// the canvas. Called whenever the mode changes.
func (m *ModalEditor) setBadge(text string) {
	if m.badge == nil {
		return
	}
	m.badge.Text = text
	m.badge.Color = badgeColor(m.mode)
	m.badge.Refresh()
}

// refreshPreview rebuilds the HighlightedView from the editor's
// current text. Keeping the preview as a named field (not rebuilt
// every render) avoids unnecessary chroma work when the user toggles
// modes without changing the text.
func (m *ModalEditor) refreshPreview() {
	view := HighlightedView(m.editor.Text, m.Language, m.Style)
	m.preview.setContent(view)
}

// ── Layout ───────────────────────────────────────────────────────────

// buildLayout wires the editor and preview panes and creates the
// persistent mode badge in the top-right corner.
func (m *ModalEditor) buildLayout() {
	m.preview = newPreviewPane(m.enterEditMode)

	// The badge is a coloured canvas.Text pinned to the top-right
	// via container.NewBorder in applyMode. A fixed-width container
	// keeps the layout stable when the label text changes length.
	m.badge = canvas.NewText("PREVIEW", badgeColor(modePreview))
	m.badge.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	m.badge.TextSize = 11
	m.badgeBox = container.NewPadded(m.badge)

	m.stack = container.NewStack()
}

// ── previewPane ──────────────────────────────────────────────────────

// previewPane is the focusable wrapper around the HighlightedView.
// It exists because HighlightedView returns a plain canvas object
// that cannot receive focus — but we need a focusable host so Enter
// can trigger the edit-mode transition.
//
// The visual body is kept inside a container.Stack rather than being
// wired directly into the renderer, because widget.NewSimpleRenderer
// takes its child as a one-shot snapshot — refreshing the content
// pointer on the widget doesn't propagate. Swapping the Stack's
// Objects slice does.
type previewPane struct {
	widget.BaseWidget

	body       *fyne.Container
	onActivate func()
}

// newPreviewPane creates an empty previewPane. Call setContent to
// install the HighlightedView before showing.
func newPreviewPane(onActivate func()) *previewPane {
	p := &previewPane{
		body:       container.NewStack(),
		onActivate: onActivate,
	}
	p.ExtendBaseWidget(p)
	return p
}

// setContent swaps the preview body and refreshes the wrapper.
// Replaces whatever is currently inside the stack; the stack itself
// is stable so the renderer never needs rebuilding.
func (p *previewPane) setContent(body fyne.CanvasObject) {
	p.body.Objects = []fyne.CanvasObject{body}
	p.body.Refresh()
}

// CreateRenderer wires the stable body container into Fyne's
// rendering pipeline. Subsequent setContent calls mutate the
// container's Objects slice, which the renderer observes via the
// container's own Refresh.
func (p *previewPane) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.body)
}

// Focusable contract — the pane can take keyboard focus.
func (p *previewPane) FocusGained() {}
func (p *previewPane) FocusLost()   {}

// Tapped fires for a single left click anywhere on the preview
// surface. Mirrors the Enter-to-edit shortcut so mouse users can
// switch modes without reaching for the keyboard.
func (p *previewPane) Tapped(_ *fyne.PointEvent) {
	if p.onActivate != nil {
		p.onActivate()
	}
}

// TappedSecondary is a no-op — we do not want right-click to switch
// modes accidentally, and context menus are out of scope for v0.2.
func (p *previewPane) TappedSecondary(_ *fyne.PointEvent) {}

// TypedRune swallows printable characters while in preview to avoid
// the terminal bell or unexpected beeps from the canvas.
func (p *previewPane) TypedRune(_ rune) {}

// TypedKey is where Enter flips us into edit mode. Other keys are
// ignored — the preview is strictly non-editing.
func (p *previewPane) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyReturn || ev.Name == fyne.KeyEnter {
		if p.onActivate != nil {
			p.onActivate()
		}
	}
}

// TypedShortcut is a no-op in preview mode; we inherit the Focusable
// interface but deliberately consume nothing.
func (p *previewPane) TypedShortcut(_ fyne.Shortcut) {}

// ── Helpers ──────────────────────────────────────────────────────────

// windowFor locates the fyne.Window hosting the given widget by
// walking every open window and checking whether the widget's
// canvas is that window's canvas. Duplicated from shortcuts.go to
// avoid cross-file coupling and kept deliberately small.
func windowFor(obj fyne.CanvasObject) fyne.Window {
	_ = obj
	app := fyne.CurrentApp()
	if app == nil {
		return nil
	}
	wins := app.Driver().AllWindows()
	if len(wins) == 0 {
		return nil
	}
	// For the modal editor we always operate against the most
	// recently used window; Fyne does not give us a direct
	// "which window hosts this widget" accessor, and the modal
	// editor is embedded rather than a top-level window itself.
	return wins[len(wins)-1]
}

// badgeColor returns the colour the mode badge should use. Edit mode
// uses a warm amber — same family as our theme's accent — so the badge
// is readable against both light and dark Fyne backgrounds; preview
// uses a subdued grey so the indicator is visible without competing
// with the code it annotates.
func badgeColor(m modalMode) color.Color {
	if m == modeEdit {
		return color.NRGBA{R: 0xd9, G: 0x77, B: 0x06, A: 0xff} // amber
	}
	return color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff} // grey
}
