package aiassist

// chat_panel.go — Left panel: conversation history with styled message bubbles.
//
// Each message is rendered as a rounded card with role-specific colours.
// The body of a bubble is a widget.RichText so that Markdown emitted by the
// LLM (bold, italic, code spans, lists, headings, code blocks) renders
// properly instead of showing raw asterisks.
//
// Every bubble also accepts a right-click that opens a context menu with a
// "Copy message" item — Fyne's RichText offers no native text selection,
// so the explicit copy action is the only way the user can get the text
// onto the clipboard.

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// messageKind distinguishes the visual style of a chat bubble.
type messageKind int

const (
	kindUser      messageKind = iota // right-aligned, accent colour
	kindAssistant                    // left-aligned, surface colour
	kindError                        // left-aligned, error colour
)

// chatPanel manages the scrollable conversation history.
type chatPanel struct {
	scroll  *container.Scroll
	content *fyne.Container
}

// newChatPanel creates an empty chat panel ready to receive messages.
func newChatPanel() *chatPanel {
	content := container.NewVBox()
	scroll := container.NewVScroll(content)
	return &chatPanel{scroll: scroll, content: content}
}

// canvasObject returns the panel's root widget for embedding in a layout.
func (p *chatPanel) canvasObject() fyne.CanvasObject {
	return p.scroll
}

// addUserMessage appends a user bubble.
func (p *chatPanel) addUserMessage(text string) {
	p.addBubble("You", text, kindUser)
}

// addAssistantMessage appends an assistant bubble.
func (p *chatPanel) addAssistantMessage(role, text string) {
	p.addBubble(role, text, kindAssistant)
}

// addError appends an error bubble.
func (p *chatPanel) addError(text string) {
	p.addBubble("Error", text, kindError)
}

// addBubble creates a styled message card and appends it to the history.
//
// The bubble layout is a stack of:
//   - a rounded rectangle background tinted by messageKind
//   - a vertical box with header (role | timestamp) and body
//
// The whole stack is wrapped in a bubbleWrapper that catches right-clicks
// so the user can copy the raw text to the clipboard. The raw text is
// captured before any Markdown rendering so the clipboard always carries
// exactly what the model sent — no widget-internal formatting.
func (p *chatPanel) addBubble(role, text string, kind messageKind) {
	ts := time.Now().Format("15:04")

	roleLabel := widget.NewLabel(role)
	roleLabel.TextStyle = fyne.TextStyle{Bold: true}

	timeLabel := widget.NewLabel(ts)
	timeLabel.TextStyle = fyne.TextStyle{Italic: true}

	body := widget.NewRichTextFromMarkdown(text)
	body.Wrapping = fyne.TextWrapWord

	header := container.NewBorder(nil, nil, roleLabel, timeLabel)
	inner := container.NewVBox(header, body)

	bg := bubbleColour(kind)
	rect := canvas.NewRectangle(bg)
	rect.CornerRadius = 8

	bubble := container.NewStack(rect, container.NewPadded(inner))
	wrapped := newBubbleWrapper(bubble, text)

	p.content.Add(wrapped)
	p.content.Refresh()
	p.scroll.ScrollToBottom()
}

// clear removes all messages from the panel.
func (p *chatPanel) clear() {
	p.content.RemoveAll()
	p.content.Refresh()
}

// bubbleColour returns the background colour for a given message kind.
func bubbleColour(kind messageKind) color.Color {
	switch kind {
	case kindUser:
		return color.NRGBA{R: 0, G: 100, B: 210, A: 40}
	case kindError:
		return color.NRGBA{R: 200, G: 40, B: 40, A: 40}
	default:
		return color.NRGBA{R: 80, G: 80, B: 80, A: 30}
	}
}

// bubbleWrapper wraps a rendered chat bubble and adds a right-click menu
// with a "Copy message" action. The raw text (pre-Markdown) is stored so
// the clipboard receives exactly what the model produced.
//
// The wrapper deliberately re-implements the narrow surface we need
// (TappedSecondary + desktop.Mouseable) instead of embedding a specific
// widget type — that keeps it composable with whatever content we hand it.
type bubbleWrapper struct {
	widget.BaseWidget
	content fyne.CanvasObject
	rawText string
}

// newBubbleWrapper builds a wrapper around content. rawText is what goes
// onto the clipboard when the user chooses "Copy message".
func newBubbleWrapper(content fyne.CanvasObject, rawText string) *bubbleWrapper {
	w := &bubbleWrapper{content: content, rawText: rawText}
	w.ExtendBaseWidget(w)
	return w
}

// CreateRenderer implements fyne.Widget by rendering the wrapped content
// as-is; the wrapper adds behaviour, not visuals.
func (w *bubbleWrapper) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.content)
}

// TappedSecondary opens the context menu at the cursor position.
func (w *bubbleWrapper) TappedSecondary(ev *fyne.PointEvent) {
	copyItem := fyne.NewMenuItem("Copy message", func() {
		windows := fyne.CurrentApp().Driver().AllWindows()
		if len(windows) == 0 {
			return
		}
		windows[0].Clipboard().SetContent(w.rawText)
	})

	menu := fyne.NewMenu("", copyItem)
	c := fyne.CurrentApp().Driver().CanvasForObject(w)
	if c != nil {
		widget.ShowPopUpMenuAtPosition(menu, c, ev.AbsolutePosition)
	}
}

// MouseDown routes secondary-button events to TappedSecondary. On some
// platforms the default Tappable dispatch misses right-clicks on custom
// widgets, so we explicitly forward them here — same pattern as in
// input_entry.go.
func (w *bubbleWrapper) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonSecondary {
		w.TappedSecondary(&fyne.PointEvent{
			Position:         ev.Position,
			AbsolutePosition: ev.AbsolutePosition,
		})
	}
}

// MouseUp is the second half of desktop.Mouseable. No behaviour needed
// here — right-click menus open on MouseDown, left-clicks pass through
// to whatever child widget is below.
func (w *bubbleWrapper) MouseUp(_ *desktop.MouseEvent) {}

// typingIndicator is a three-dot animated widget shown while the agent thinks.
type typingIndicator struct {
	widget.BaseWidget
	dots   int
	ticker *time.Ticker
	stop   chan struct{}
	label  *widget.Label
}

// newTypingIndicator creates a new typing indicator that updates itself.
func newTypingIndicator() *typingIndicator {
	t := &typingIndicator{
		label: widget.NewLabel(""),
		stop:  make(chan struct{}),
	}
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer implements fyne.Widget.
func (t *typingIndicator) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.label)
}

// Start begins the dot animation.
func (t *typingIndicator) Start() {
	t.ticker = time.NewTicker(400 * time.Millisecond)
	go func() {
		for {
			select {
			case <-t.stop:
				return
			case <-t.ticker.C:
				t.dots = (t.dots + 1) % 4
				dots := ""
				for i := 0; i < t.dots; i++ {
					dots += "●"
				}
				for i := t.dots; i < 3; i++ {
					dots += "○"
				}
				fyne.Do(func() {
					t.label.SetText(fmt.Sprintf("  %s  thinking", dots))
				})
			}
		}
	}()
}

// Stop halts the animation and hides the indicator.
func (t *typingIndicator) Stop() {
	if t.ticker != nil {
		t.ticker.Stop()
	}
	select {
	case t.stop <- struct{}{}:
	default:
	}
	fyne.Do(func() { t.label.SetText("") })
}

// thinSeparator returns a horizontal line for visual separation.
func thinSeparator() fyne.CanvasObject {
	line := canvas.NewLine(theme.ShadowColor())
	line.StrokeWidth = 1
	return line
}
