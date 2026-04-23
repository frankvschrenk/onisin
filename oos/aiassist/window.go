package aiassist

// window.go — AI Assistant main window with two modes.
//
// Mode switcher lives in the header row (modeHeader). The body is an
// AppTabs container with two tabs:
//
//   Chat     — the actual chat surface; right-side pane swaps based on mode
//               (Board → activity feed, Events → answer + sources)
//   Settings — tunable parameters; shows 2 fields in Board mode,
//               4 fields in Events mode
//
// Both modes share the same chat history, input bar and reset button —
// only the right panel and the submit logic differ.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos/helper"
)

// OpenWindow opens a new AI Assistant chat window.
// The session is initialised from the current helper.LLMUrl configuration.
func OpenWindow(app fyne.App) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := NewSession(ctx)
	if err != nil {
		return fmt.Errorf("AI assistant not available: %w", err)
	}

	title := fmt.Sprintf("OOS AI Assistant — %s", session.ModelName())
	win := app.NewWindow(title)
	win.Resize(fyne.NewSize(1100, 650))

	settings := newChatSettings()
	aw := &assistWindow{
		win:      win,
		app:      app,
		session:  session,
		chat:     newChatPanel(),
		activity: newActivityPanel(session.ModelName(), helper.LLMUrl),
		typing:   newTypingIndicator(),
		events:   newEventPanel(app),
		settings: settings,
		setPanel: newSettingsPanel(app, settings),
	}
	aw.build()
	win.Show()

	return nil
}

// assistWindow holds all state for the AI assistant window.
type assistWindow struct {
	win      fyne.Window
	app      fyne.App
	session  *Session       // Board-mode eino agent
	evSess   *eventSession  // Events-mode RAG session (lazy-initialised)
	chat     *chatPanel
	activity *activityPanel
	events   *eventPanel
	typing   *typingIndicator
	header   *modeHeader
	split    *container.Split
	settings *chatSettings  // tunable parameters (temperature, timeout, ...)
	setPanel *settingsPanel // renders the Settings tab
	input    *inputEntry
	sendBtn  *widget.Button
	busy     bool
	mu       sync.Mutex
}

// build assembles the full window layout.
func (aw *assistWindow) build() {
	// ── Input bar ─────────────────────────────────────────────────────────────
	aw.input = newInputEntry()
	aw.input.SetPlaceHolder("Ask a question... (Enter to send, Shift+Enter for newline)")
	aw.input.OnSubmitted = func(_ string) { aw.send() }

	aw.sendBtn = widget.NewButtonWithIcon("Send", theme.MailSendIcon(), aw.send)
	aw.sendBtn.Importance = widget.HighImportance

	resetBtn := widget.NewButtonWithIcon("New", theme.DeleteIcon(), aw.reset)

	inputRow := container.NewBorder(nil, nil, nil, aw.sendBtn, aw.input)

	// ── Left: chat panel ──────────────────────────────────────────────────────
	typingRow := container.NewCenter(aw.typing)
	leftBody := container.NewBorder(nil,
		container.NewVBox(typingRow, inputRow),
		nil, nil,
		aw.chat.canvasObject(),
	)
	// container.NewPadded adds theme-consistent padding on all four sides.
	// Applying it here means bubbles, input and scroll area share the same
	// breathing room — no individual widget has to manage its own spacers.
	leftPanel := container.NewPadded(leftBody)

	// ── Right: activity panel (board mode) ───────────────────────────────────
	rightBody := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil,
				widget.NewLabel("Activity"),
				resetBtn,
			),
			thinSeparator(),
		),
		nil, nil, nil,
		aw.activity.canvasObject(),
	)
	rightPanel := container.NewPadded(rightBody)

	// ── Split ─────────────────────────────────────────────────────────────────
	aw.split = container.NewHSplit(leftPanel, rightPanel)
	aw.split.SetOffset(0.65) // 65% chat, 35% activity

	// ── Mode header on top ────────────────────────────────────────────────────
	aw.header = newModeHeader(aw.onModeChange, aw.onModelChange)

	// ── Tabs: Chat + Settings ─────────────────────────────────────────────────
	// The chat tab hosts the main split; the settings tab hosts a mode-aware
	// parameter panel. Tabs keep the window uncluttered without hiding any
	// controls behind menus.
	tabs := container.NewAppTabs(
		container.NewTabItem("Chat", aw.split),
		container.NewTabItem("Settings", aw.setPanel.canvasObject()),
	)

	content := container.NewBorder(
		// Pad the header block so its content doesn't touch the window frame.
		container.NewPadded(
			container.NewVBox(aw.header.canvasObject(), thinSeparator()),
		),
		nil, nil, nil,
		tabs,
	)
	aw.win.SetContent(content)

	aw.activity.SetActive(false, "Ready")
	aw.chat.addAssistantMessage(
		"OOS",
		fmt.Sprintf("Ready — %s @ %s", aw.session.ModelName(), helper.LLMUrl),
	)
}

// onModeChange swaps the right side of the split between activity
// (board mode) and the event panel (events mode) and notifies the
// settings panel so it can show the matching set of controls.
func (aw *assistWindow) onModeChange(m chatMode) {
	fyne.Do(func() {
		switch m {
		case modeEvents:
			aw.split.Trailing = aw.events.canvasObject()
		default:
			aw.split.Trailing = aw.activity.canvasObject()
		}
		aw.split.Refresh()
		if aw.setPanel != nil {
			aw.setPanel.SetMode(m)
		}
	})
}

// onModelChange rebuilds both sessions against a newly-picked model.
//
// Board session is rebuilt synchronously because the chat panel already
// references its name; event session is invalidated so the next event-mode
// send re-creates it with the new model. Running history is cleared because
// mixing models in one thread confuses most backends.
func (aw *assistWindow) onModelChange(modelName string) {
	if modelName == "" {
		return
	}
	if aw.session != nil && aw.session.ModelName() == modelName {
		return // same model — nothing to do
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		newSess, err := NewSessionWithModel(ctx, modelName)
		if err != nil {
			fyne.Do(func() {
				aw.chat.addError(fmt.Sprintf("Model switch failed: %s", err.Error()))
			})
			return
		}

		aw.mu.Lock()
		aw.session = newSess
		aw.evSess = nil // force lazy re-init on next event-mode send
		aw.mu.Unlock()

		fyne.Do(func() {
			aw.win.SetTitle(fmt.Sprintf("OOS AI Assistant — %s", modelName))
			aw.chat.clear()
			aw.events.Clear()
			aw.activity.Clear()
			aw.activity.UpdateSystemInfo(0, "—")
			aw.chat.addAssistantMessage("OOS",
				fmt.Sprintf("Model switched to %s. Conversation reset.", modelName))
		})
	}()
}

// send reads the input and dispatches it to the right pipeline depending on the mode.
func (aw *assistWindow) send() {
	aw.mu.Lock()
	if aw.busy {
		aw.mu.Unlock()
		return
	}
	text := strings.TrimSpace(aw.input.Text)
	if text == "" {
		aw.mu.Unlock()
		return
	}
	aw.busy = true
	aw.mu.Unlock()

	aw.input.SetText("")
	aw.sendBtn.Disable()

	if aw.header != nil && aw.header.Mode() == modeEvents {
		aw.sendEvent(text)
		return
	}
	aw.sendBoard(text)
}

// sendBoard runs the board-mode eino agent turn.
func (aw *assistWindow) sendBoard(text string) {
	fyne.Do(func() {
		aw.chat.addUserMessage(text)
		aw.activity.AddDivider(text)
		aw.activity.SetActive(true, "Thinking...")
		aw.typing.Start()
	})

	go func() {
		defer func() {
			aw.mu.Lock()
			aw.busy = false
			aw.mu.Unlock()
			fyne.Do(func() {
				aw.sendBtn.Enable()
				aw.typing.Stop()
				aw.activity.SetActive(false, "Idle")
				aw.activity.UpdateSystemInfo(aw.session.MessageCount(), "")
			})
		}()

		ctx, cancel := context.WithTimeout(context.Background(), aw.settings.Timeout())
		defer cancel()

		response, err := aw.session.Send(ctx, text,
			func(ev ToolEvent) {
				// onToolStart — fires immediately before the tool executes
				fyne.Do(func() {
					aw.activity.SetActive(true, "Running "+ev.Name+"...")
					aw.activity.AddToolCall(ev.Name)
				})
				globalDebug.Add(ev.Name, ev.Input, "", false)
			},
			func(ev ToolEvent) {
				// onToolEnd — ev.Input holds the tool result string
				globalDebug.AddResult(ev.Name, ev.Input)
				fyne.Do(func() {
					aw.activity.AddResult(ev.Name + " done")
				})
			},
		)

		fyne.Do(func() {
			if err != nil {
				aw.chat.addError(err.Error())
				aw.activity.AddResult("error: " + err.Error())
			} else {
				aw.chat.addAssistantMessage(aw.session.ModelName(), response)
				aw.activity.AddResult("done")
			}
		})
	}()
}

// sendEvent runs one RAG turn against the event pipeline.
//
// The event session is created lazily on the first call so opening the
// window doesn't cost anything as long as the user stays in board mode.
func (aw *assistWindow) sendEvent(question string) {
	mapping := aw.header.Mapping()
	if mapping == "" {
		fyne.Do(func() {
			aw.chat.addError("Please pick a context (event mapping) before asking.")
			aw.sendBtn.Enable()
		})
		aw.mu.Lock()
		aw.busy = false
		aw.mu.Unlock()
		return
	}
	streamID := aw.header.StreamID()

	fyne.Do(func() {
		aw.chat.addUserMessage(question)
		aw.events.SetAnswer("_Searching events..._")
		aw.events.SetSources(nil)
		aw.typing.Start()
	})

	go func() {
		defer func() {
			aw.mu.Lock()
			aw.busy = false
			aw.mu.Unlock()
			fyne.Do(func() {
				aw.sendBtn.Enable()
				aw.typing.Stop()
			})
		}()

		ctx, cancel := context.WithTimeout(context.Background(), aw.settings.Timeout())
		defer cancel()

		// Lazy-init the event session on first use.
		// Use the model currently selected in the header so a mid-window
		// model switch takes effect immediately, not just in board mode.
		if aw.evSess == nil {
			s, err := newEventSessionWithModel(ctx, aw.header.Model())
			if err != nil {
				fyne.Do(func() {
					aw.events.SetAnswer(fmt.Sprintf("**LLM unavailable:** %s", err.Error()))
					aw.chat.addError(err.Error())
				})
				return
			}
			aw.evSess = s
		}

		fyne.Do(func() {
			aw.events.SetAnswer(fmt.Sprintf("_Asking LLM (%s)..._", aw.evSess.ModelName()))
		})

		ans, err := aw.evSess.Ask(ctx, EventQuery{
			Mapping:     mapping,
			StreamID:    streamID,
			Question:    question,
			Limit:       aw.settings.TopHits(),
			MaxTokens:   aw.settings.MaxTokens(),
			Temperature: aw.settings.Temperature(),
		})
		if err != nil {
			fyne.Do(func() {
				aw.events.SetAnswer(fmt.Sprintf("**Error:** %s", err.Error()))
				aw.chat.addError(err.Error())
			})
			return
		}

		fyne.Do(func() {
			aw.events.SetAnswer(ans.Text)
			aw.events.SetSources(ans.Hits)
			aw.chat.addAssistantMessage(ans.Model, ans.Text)
		})
	}()
}

// reset clears conversation history and both panels regardless of mode.
func (aw *assistWindow) reset() {
	aw.session.Reset()
	fyne.Do(func() {
		aw.chat.clear()
		aw.activity.Clear()
		aw.events.Clear()
		aw.activity.SetActive(false, "Ready")
		aw.activity.UpdateSystemInfo(0, "—")
		aw.chat.addAssistantMessage("OOS",
			fmt.Sprintf("Ready — %s @ %s", aw.session.ModelName(), helper.LLMUrl))
	})
}
