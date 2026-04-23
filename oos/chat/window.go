package chat

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

var (
	chatWindowMu    sync.Mutex
	chatWindowCount int
)

func OpenChatWindow(app fyne.App, client LLMClient) {
	chatWindowMu.Lock()
	chatWindowCount++
	num := chatWindowCount
	chatWindowMu.Unlock()

	title := fmt.Sprintf("OOS Chat — %s", client.ModelName())
	if num > 1 {
		title = fmt.Sprintf("OOS Chat %d — %s", num, client.ModelName())
	}

	win := app.NewWindow(title)
	win.Resize(fyne.NewSize(700, 550))

	cw := &chatWindow{win: win, client: client}
	cw.build()

	win.SetOnClosed(func() {
		chatWindowMu.Lock()
		chatWindowCount--
		chatWindowMu.Unlock()
	})

	win.Show()
}

type chatWindow struct {
	win    fyne.Window
	client LLMClient

	session      *Session
	historyText  string
	historyLabel *widget.Label
	input        *widget.Entry
	sendBtn      *widget.Button
	toolsBtn     *widget.Button
	useTools     bool
	busy         bool
	mu           sync.Mutex
}

func (cw *chatWindow) build() {
	cw.historyLabel = widget.NewLabel("")
	cw.historyLabel.Wrapping = fyne.TextWrapWord
	historyScroll := container.NewVScroll(cw.historyLabel)

	cw.input = widget.NewMultiLineEntry()
	cw.input.SetPlaceHolder("Nachricht eingeben...")
	cw.input.Wrapping = fyne.TextWrapWord
	cw.input.OnSubmitted = func(_ string) { cw.send() }

	cw.sendBtn = widget.NewButtonWithIcon("Senden", theme.MailSendIcon(), cw.send)

	cw.useTools = true
	cw.toolsBtn = widget.NewButton("Tools: AN", func() {
		cw.useTools = !cw.useTools
		if cw.useTools {
			cw.toolsBtn.SetText("Tools: AN")
		} else {
			cw.toolsBtn.SetText("Tools: AUS")
		}
		cw.resetSession()
	})

	resetBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		cw.resetSession()
		cw.historyText = ""
		cw.historyLabel.SetText("")
		cw.appendStatus("Gespräch zurückgesetzt.")
	})

	modelLabel := widget.NewLabel(fmt.Sprintf("Modell: %s", cw.client.ModelName()))

	header := container.NewBorder(nil, nil, modelLabel, container.NewHBox(cw.toolsBtn, resetBtn))
	inputArea := container.NewBorder(nil, nil, nil, cw.sendBtn, cw.input)
	content := container.NewBorder(header, inputArea, nil, nil, historyScroll)

	cw.win.SetContent(content)
	cw.resetSession()
	cw.appendStatus(fmt.Sprintf("Verbunden mit %s. Tools: AN", cw.client.ModelName()))
}

func (cw *chatWindow) resetSession() {
	cw.session = NewSession(cw.client, buildSystemPrompt(), cw.useTools)
}

func (cw *chatWindow) send() {
	cw.mu.Lock()
	if cw.busy {
		cw.mu.Unlock()
		return
	}
	text := strings.TrimSpace(cw.input.Text)
	if text == "" {
		cw.mu.Unlock()
		return
	}
	cw.busy = true
	cw.mu.Unlock()

	cw.input.SetText("")
	cw.sendBtn.Disable()
	cw.appendMessage("Du", text)

	go func() {
		defer func() {
			cw.mu.Lock()
			cw.busy = false
			cw.mu.Unlock()
			fyne.Do(func() { cw.sendBtn.Enable() })
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		fyne.Do(func() { cw.appendStatus("...") })

		response, err := cw.session.Send(
			ctx,
			text,
			nil,
			func(toolName string) {
				fyne.Do(func() { cw.appendStatus(fmt.Sprintf("→ %s", toolName)) })
			},
		)

		fyne.Do(func() {
			cw.historyText = strings.TrimSuffix(cw.historyText, "  ℹ ...\n")
			if err != nil {
				cw.appendMessage(cw.client.ModelName(), "❌ Fehler: "+err.Error())
			} else {
				cw.appendMessage(cw.client.ModelName(), response)
			}
		})
	}()
}

func (cw *chatWindow) appendMessage(role, text string) {
	cw.historyText += fmt.Sprintf("\n[%s]\n%s\n", role, text)
	cw.historyLabel.SetText(cw.historyText)
}

func (cw *chatWindow) appendStatus(text string) {
	cw.historyText += fmt.Sprintf("  ℹ %s\n", text)
	cw.historyLabel.SetText(cw.historyText)
}

func buildSystemPrompt() string {
	return fmt.Sprintf(`Du bist OOS, ein Datenassistent für %s (Rolle: %s).

REGELN die du IMMER befolgen musst:
1. Antworte IMMER auf Deutsch, egal in welcher Sprache gefragt wird
2. Wenn der Benutzer Daten sehen will, rufe SOFORT oos_query auf - frage nicht nach
3. Nutze für Listen immer context="*_list" und für Details "*_detail"
4. Schreibe NIEMALS Daten ohne explizite Bestätigung des Benutzers
5. Gib KEINE Erklärungen was du tun wirst - tue es einfach

VERFÜGBARE TOOLS:
- oos_query(context, query): Lädt Daten und zeigt sie in einem Fenster
- oos_render(context, dsl, json): Rendert eigene Ansichten
- oos_stream_append(stream, event_type, data, text): Schreibt Events
- oos_vector_search(query, collection): Semantische Suche

NIEMALS: nachfragen, erklären, auf andere Sprachen antworten.`,
		helper.ActiveUsername(), helper.ActiveRole)
}
