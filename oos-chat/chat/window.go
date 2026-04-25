package chat

// window.go — single-mode chat window opened by host applications.
//
// The window holds a chat transcript on the left and a small
// activity feed on the right that shows tool calls as they happen.
// The mode is fixed for the window's lifetime: callers that want
// multiple modes open multiple windows.
//
// Layout:
//   ┌────────────────────────────┬──────────────────┐
//   │ chat history               │ activity feed    │
//   │ ...                        │ • tool: dsl_..   │
//   │ [user input         ] [↑]  │ • result: ...    │
//   └────────────────────────────┴──────────────────┘

import (
	"context"
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// OpenWindow creates a new chat window driven by the given mode and
// returns it ready to Show(). The caller is responsible for the
// Show()/Close() lifecycle so a host can keep a reference and
// re-focus the window instead of opening duplicates.
func OpenWindow(app fyne.App, mode Mode) (fyne.Window, error) {
	if mode == nil {
		return nil, fmt.Errorf("oos-chat: mode is required")
	}
	ctx := context.Background()
	session, err := NewSession(ctx, mode, "")
	if err != nil {
		return nil, err
	}

	w := app.NewWindow("AI Chat — " + mode.Name())
	w.Resize(fyne.NewSize(1000, 700))

	chat := newChatPanel()
	activity := newActivityPanel(session.ModelName())

	input := widget.NewMultiLineEntry()
	input.SetPlaceHolder("Frage stellen oder Auftrag formulieren …")
	input.SetMinRowsVisible(2)

	var busy sync.Mutex
	sendBtn := widget.NewButtonWithIcon("", theme.MailSendIcon(), nil)
	sendBtn.Importance = widget.HighImportance

	doSend := func() {
		text := input.Text
		if text == "" {
			return
		}
		if !busy.TryLock() {
			return
		}
		input.SetText("")
		chat.addUser(text)
		activity.startTurn(text)
		sendBtn.Disable()

		go func() {
			defer busy.Unlock()
			defer fyne.Do(func() { sendBtn.Enable() })

			reply, err := session.Send(ctx, text,
				func(ev ToolEvent) {
					fyne.Do(func() { activity.addToolStart(ev.Name, ev.Input) })
				},
				func(ev ToolEvent) {
					fyne.Do(func() { activity.addToolEnd(ev.Name, ev.Input) })
				},
			)
			if err != nil {
				fyne.Do(func() { chat.addError(err.Error()) })
				return
			}
			fyne.Do(func() {
				chat.addAssistant(reply)
				if err := mode.OnAssistantMessage(reply); err != nil {
					chat.addError("OnAssistantMessage: " + err.Error())
				}
			})
		}()
	}

	sendBtn.OnTapped = doSend

	inputBar := container.NewBorder(nil, nil, nil, sendBtn, input)

	left := container.NewBorder(nil, inputBar, nil, nil, chat.widget())
	split := container.NewHSplit(left, activity.widget())
	split.SetOffset(0.7)

	w.SetContent(split)
	return w, nil
}

// ── chat panel ────────────────────────────────────────────────────

type chatPanel struct {
	box    *fyne.Container
	scroll *container.Scroll
}

func newChatPanel() *chatPanel {
	box := container.NewVBox()
	scroll := container.NewVScroll(box)
	return &chatPanel{box: box, scroll: scroll}
}

func (p *chatPanel) widget() fyne.CanvasObject { return p.scroll }

func (p *chatPanel) addUser(text string)      { p.add("Du", text, color.NRGBA{0xE3, 0xF2, 0xFD, 0xff}) }
func (p *chatPanel) addAssistant(text string) { p.add("Assistent", text, color.NRGBA{0xF1, 0xF8, 0xE9, 0xff}) }
func (p *chatPanel) addError(text string)     { p.add("Fehler", text, color.NRGBA{0xFF, 0xEB, 0xEE, 0xff}) }

func (p *chatPanel) add(role, text string, bg color.Color) {
	header := widget.NewLabelWithStyle(role, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	body := widget.NewLabel(text)
	body.Wrapping = fyne.TextWrapWord
	rect := canvas.NewRectangle(bg)
	card := container.NewStack(rect, container.NewPadded(container.NewVBox(header, body)))
	p.box.Add(card)
	p.scroll.ScrollToBottom()
}

// ── activity panel ────────────────────────────────────────────────

type activityPanel struct {
	box    *fyne.Container
	scroll *container.Scroll
	model  *widget.Label
}

func newActivityPanel(modelName string) *activityPanel {
	box := container.NewVBox()
	model := widget.NewLabel("Modell: " + modelName)
	header := container.NewVBox(
		widget.NewLabelWithStyle("Aktivität", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		model,
		widget.NewSeparator(),
	)
	scroll := container.NewVScroll(container.NewBorder(header, nil, nil, nil, box))
	return &activityPanel{box: box, scroll: scroll, model: model}
}

func (p *activityPanel) widget() fyne.CanvasObject { return p.scroll }

func (p *activityPanel) startTurn(userText string) {
	p.box.Add(widget.NewSeparator())
	hint := widget.NewLabel("▶ " + truncate(userText, 60))
	hint.TextStyle = fyne.TextStyle{Italic: true}
	p.box.Add(hint)
	p.scroll.ScrollToBottom()
}

func (p *activityPanel) addToolStart(name, input string) {
	p.box.Add(widget.NewLabel("• " + name + "(" + truncate(oneLine(input), 80) + ")"))
	p.scroll.ScrollToBottom()
}

func (p *activityPanel) addToolEnd(name, output string) {
	label := widget.NewLabel("   ↳ " + truncate(oneLine(output), 100))
	label.Wrapping = fyne.TextWrapWord
	p.box.Add(label)
	p.scroll.ScrollToBottom()
}

// ── helpers ────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if n <= 0 || len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func oneLine(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			out = append(out, ' ')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
