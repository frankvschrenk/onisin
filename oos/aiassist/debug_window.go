package aiassist

// debug_window.go — Developer debug window for the OOS AI assistant.
//
// Shows all recorded tool calls as a single, selectable, copyable text
// document in a MultiLineEntry widget. The entry makes it trivial for a
// user to highlight a problematic interaction and paste it into an email
// or ticket for an admin to investigate.
//
// The document is rebuilt on every update from the globalDebug log so
// the view always reflects the current state. A format toggle switches
// between a human-readable and a JSON representation — JSON is handy
// for pasting into tools that expect structured input.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// debugEvent is a single recorded tool interaction.
type debugEvent struct {
	Time   time.Time `json:"time"`
	Tool   string    `json:"tool"`
	Input  string    `json:"input"`
	Output string    `json:"output"`
	IsErr  bool      `json:"is_error,omitempty"`
}

// debugLog collects debug events and notifies the window when new ones arrive.
type debugLog struct {
	mu     sync.Mutex
	events []debugEvent
	notify func()
}

// globalDebug is the shared debug log for the current session.
var globalDebug = &debugLog{}

// Add appends a tool-start event.
func (d *debugLog) Add(toolName, input, output string, isErr bool) {
	d.mu.Lock()
	d.events = append(d.events, debugEvent{
		Time:  time.Now(),
		Tool:  toolName,
		Input: input,
		IsErr: isErr,
	})
	d.mu.Unlock()
	d.ping()
}

// AddResult sets the output on the most recent event for toolName.
func (d *debugLog) AddResult(toolName, output string) {
	d.mu.Lock()
	for i := len(d.events) - 1; i >= 0; i-- {
		if d.events[i].Tool == toolName && d.events[i].Output == "" {
			d.events[i].Output = output
			break
		}
	}
	d.mu.Unlock()
	d.ping()
}

// Clear removes all events.
func (d *debugLog) Clear() {
	d.mu.Lock()
	d.events = nil
	d.mu.Unlock()
	d.ping()
}

// snapshot returns a copy of all current events.
func (d *debugLog) snapshot() []debugEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]debugEvent, len(d.events))
	copy(out, d.events)
	return out
}

func (d *debugLog) ping() {
	if d.notify != nil {
		d.notify()
	}
}

// ── Debug window ──────────────────────────────────────────────────────────────

// debugFormat is the current rendering mode of the debug view.
type debugFormat int

const (
	formatText debugFormat = iota // human-readable
	formatJSON                    // structured array
)

// debugWindow displays all recorded tool calls in a selectable text area.
type debugWindow struct {
	win    fyne.Window
	entry  *widget.Entry
	format debugFormat
}

// OpenDebugWindow opens a new debug window backed by globalDebug.
func OpenDebugWindow(app fyne.App) {
	dw := &debugWindow{format: formatText}
	dw.build(app)
	dw.win.Show()

	globalDebug.notify = func() {
		fyne.Do(dw.refresh)
	}
	dw.refresh()
}

func (dw *debugWindow) build(app fyne.App) {
	dw.win = app.NewWindow("OOS AI Debug")
	dw.win.Resize(fyne.NewSize(900, 650))

	// A plain MultiLineEntry gives us selection, copy-to-clipboard and
	// scrolling for free on every platform Fyne supports — desktop and
	// mobile alike. That's all we need for a debug log.
	dw.entry = widget.NewMultiLineEntry()
	dw.entry.Wrapping = fyne.TextWrapWord

	// Header controls: title on the left, format toggle + clear on the right.
	title := widget.NewLabel("Tool calls & results")
	title.TextStyle = fyne.TextStyle{Bold: true}

	formatBtn := widget.NewButton("Format: Text", nil)
	formatBtn.OnTapped = func() {
		if dw.format == formatText {
			dw.format = formatJSON
			formatBtn.SetText("Format: JSON")
		} else {
			dw.format = formatText
			formatBtn.SetText("Format: Text")
		}
		dw.refresh()
	}

	clearBtn := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		globalDebug.Clear()
	})

	header := container.NewBorder(nil, nil,
		title,
		container.NewHBox(formatBtn, clearBtn),
	)

	dw.win.SetContent(container.NewBorder(
		container.NewVBox(header, thinSeparator()),
		nil, nil, nil,
		dw.entry,
	))

	dw.win.SetOnClosed(func() {
		globalDebug.notify = nil
	})
}

// refresh rebuilds the entry content from the current event log.
func (dw *debugWindow) refresh() {
	events := globalDebug.snapshot()

	var content string
	switch dw.format {
	case formatJSON:
		content = renderJSON(events)
	default:
		content = renderText(events)
	}

	dw.entry.SetText(content)
}

// renderText produces a human-readable dump of all events.
//
// Each event gets a short divider line, a header row with the tool name
// and timestamp, and clearly labelled INPUT / OUTPUT sections. Long
// outputs are not truncated — the whole point of the entry is that the
// user can scroll, search and copy any piece of text.
func renderText(events []debugEvent) string {
	if len(events) == 0 {
		return "No tool calls yet.\n"
	}

	var sb strings.Builder
	for i, ev := range events {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Repeat("─", 80))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("[%s] %s", ev.Time.Format("15:04:05"), ev.Tool))
		if ev.IsErr {
			sb.WriteString("  (ERROR)")
		}
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("─", 80))
		sb.WriteString("\n")

		sb.WriteString("\nINPUT:\n")
		sb.WriteString(ev.Input)
		if !strings.HasSuffix(ev.Input, "\n") {
			sb.WriteString("\n")
		}

		sb.WriteString("\nOUTPUT:\n")
		if ev.Output == "" {
			sb.WriteString("(waiting for result...)\n")
		} else {
			sb.WriteString(ev.Output)
			if !strings.HasSuffix(ev.Output, "\n") {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

// renderJSON produces an indented JSON array of all events.
// Useful when the user wants to feed the log into another tool.
func renderJSON(events []debugEvent) string {
	if len(events) == 0 {
		return "[]\n"
	}
	b, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return fmt.Sprintf("// error marshalling events: %v\n", err)
	}
	return string(b) + "\n"
}
