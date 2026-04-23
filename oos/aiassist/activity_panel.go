package aiassist

// activity_panel.go — Right panel: live agent activity feed with pulse animation.
//
// Shows tool calls, arguments, results and system info in real time.
// Uses a canvas.Circle pulse animation to indicate when the agent is active.

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// activityPanel shows live agent activity on the right side of the window.
type activityPanel struct {
	// pulse animation
	pulse     *pulseIndicator
	statusLbl *widget.Label

	// activity feed
	feedContent *fyne.Container
	feedScroll  *container.Scroll

	// system info
	modelLbl    *widget.Label
	endpointLbl *widget.Label
	messagesLbl *widget.Label
	tokensLbl   *widget.Label
}

// newActivityPanel creates the right-side panel.
func newActivityPanel(modelName, endpoint string) *activityPanel {
	p := &activityPanel{
		pulse:       newPulseIndicator(),
		statusLbl:   widget.NewLabel("Idle"),
		feedContent: container.NewVBox(),
		modelLbl:    widget.NewLabel(modelName),
		endpointLbl: widget.NewLabel(endpoint),
		messagesLbl: widget.NewLabel("0"),
		tokensLbl:   widget.NewLabel("—"),
	}
	p.feedScroll = container.NewVScroll(p.feedContent)
	p.statusLbl.TextStyle = fyne.TextStyle{Italic: true}
	return p
}

// canvasObject builds and returns the full right panel layout.
func (p *activityPanel) canvasObject() fyne.CanvasObject {
	// ── Pulse + status ────────────────────────────────────────────────────────
	pulseRow := container.NewBorder(nil, nil, p.pulse.canvasObject(), nil,
		container.NewPadded(p.statusLbl))

	// ── Activity feed ─────────────────────────────────────────────────────────
	activityHeader := sectionHeader("ACTIVITY")

	// ── System info ───────────────────────────────────────────────────────────
	systemHeader := sectionHeader("SYSTEM")
	systemGrid := container.NewVBox(
		infoRow("Model", p.modelLbl),
		infoRow("Endpoint", p.endpointLbl),
		infoRow("Messages", p.messagesLbl),
		infoRow("Tokens", p.tokensLbl),
	)

	return container.NewBorder(
		container.NewVBox(pulseRow, thinSeparator(), activityHeader),
		container.NewVBox(thinSeparator(), systemHeader, systemGrid),
		nil, nil,
		p.feedScroll,
	)
}

// SetActive switches the pulse animation and status label on or off.
func (p *activityPanel) SetActive(active bool, status string) {
	fyne.Do(func() {
		p.statusLbl.SetText(status)
		if active {
			p.pulse.Start()
		} else {
			p.pulse.Stop()
		}
	})
}

// AddToolCall appends a tool call entry to the activity feed.
func (p *activityPanel) AddToolCall(toolName string) {
	fyne.Do(func() {
		entry := p.makeToolEntry(toolName)
		p.feedContent.Add(entry)
		p.feedContent.Refresh()
		p.feedScroll.ScrollToBottom()
	})
}

// AddResult appends a short result line to the activity feed.
func (p *activityPanel) AddResult(text string) {
	fyne.Do(func() {
		lbl := widget.NewLabel("✅ " + text)
		lbl.TextStyle = fyne.TextStyle{Italic: true}
		p.feedContent.Add(lbl)
		p.feedContent.Refresh()
		p.feedScroll.ScrollToBottom()
	})
}

// AddDivider appends a timestamped section divider (one per user turn).
func (p *activityPanel) AddDivider(userMsg string) {
	fyne.Do(func() {
		ts := time.Now().Format("15:04:05")
		short := userMsg
		if len(short) > 40 {
			short = short[:37] + "..."
		}
		lbl := widget.NewLabel(fmt.Sprintf("── %s  %s", ts, short))
		lbl.TextStyle = fyne.TextStyle{Bold: true}
		p.feedContent.Add(lbl)
		p.feedContent.Add(thinSeparator())
		p.feedContent.Refresh()
		p.feedScroll.ScrollToBottom()
	})
}

// UpdateSystemInfo refreshes the system info section.
func (p *activityPanel) UpdateSystemInfo(messages int, tokens string) {
	fyne.Do(func() {
		p.messagesLbl.SetText(fmt.Sprintf("%d", messages))
		if tokens != "" {
			p.tokensLbl.SetText(tokens)
		}
	})
}

// Clear removes all activity feed entries.
func (p *activityPanel) Clear() {
	fyne.Do(func() {
		p.feedContent.RemoveAll()
		p.feedContent.Refresh()
	})
}

// makeToolEntry builds a compact tool call card.
func (p *activityPanel) makeToolEntry(toolName string) fyne.CanvasObject {
	icon := toolIcon(toolName)
	nameLbl := widget.NewLabel(icon + " " + toolName)
	nameLbl.TextStyle = fyne.TextStyle{Bold: true}

	ts := widget.NewLabel(time.Now().Format("15:04:05"))
	ts.TextStyle = fyne.TextStyle{Italic: true}

	bg := canvas.NewRectangle(color.NRGBA{R: 0, G: 180, B: 120, A: 25})
	bg.CornerRadius = 6

	inner := container.NewBorder(nil, nil, nameLbl, ts)
	return container.NewStack(bg, container.NewPadded(inner))
}

// toolIcon returns a relevant emoji for a given tool name.
func toolIcon(name string) string {
	switch name {
	case "oos_query":
		return "🔍"
	case "oos_ui_save":
		return "💾"
	case "oos_delete":
		return "🗑"
	case "oos_new":
		return "➕"
	case "oos_render":
		return "🖼"
	case "oos_stream_append":
		return "📝"
	case "oos_vector_search":
		return "🧲"
	case "oos_system_status":
		return "📊"
	default:
		return "⚙️"
	}
}

// sectionHeader returns a bold section label.
func sectionHeader(title string) fyne.CanvasObject {
	lbl := widget.NewLabel(title)
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	return lbl
}

// infoRow returns a two-column info line.
func infoRow(label string, value *widget.Label) fyne.CanvasObject {
	key := widget.NewLabel(label + ":")
	key.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewGridWithColumns(2, key, value)
}

// ── Pulse indicator ───────────────────────────────────────────────────────────

// pulseIndicator is a small animated circle that pulses while the agent is active.
type pulseIndicator struct {
	circle *canvas.Circle
	stop   chan struct{}
	active bool
}

const pulseSize = float32(14)

// newPulseIndicator creates a new pulse indicator in idle state.
func newPulseIndicator() *pulseIndicator {
	c := canvas.NewCircle(idleColour())
	return &pulseIndicator{circle: c, stop: make(chan struct{}, 1)}
}

// canvasObject returns the circle wrapped in a fixed-size container.
func (p *pulseIndicator) canvasObject() fyne.CanvasObject {
	p.circle.Resize(fyne.NewSize(pulseSize, pulseSize))
	return container.NewGridWrap(fyne.NewSize(pulseSize, pulseSize), p.circle)
}

// Start begins the pulse animation.
func (p *pulseIndicator) Start() {
	if p.active {
		return
	}
	p.active = true
	// drain stop channel
	select {
	case <-p.stop:
	default:
	}
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		step := 0.0
		for {
			select {
			case <-p.stop:
				fyne.Do(func() {
					p.circle.FillColor = idleColour()
					p.circle.Refresh()
				})
				return
			case <-ticker.C:
				step += 0.12
				alpha := uint8(180 + 75*math.Sin(step))
				fyne.Do(func() {
					p.circle.FillColor = color.NRGBA{R: 0, G: 200, B: 120, A: alpha}
					p.circle.Refresh()
				})
			}
		}
	}()
}

// Stop halts the animation and resets to idle colour.
func (p *pulseIndicator) Stop() {
	if !p.active {
		return
	}
	p.active = false
	p.stop <- struct{}{}
}

// idleColour returns the resting colour of the pulse dot.
func idleColour() color.Color {
	return theme.DisabledColor()
}
