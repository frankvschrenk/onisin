package aiassist

// settings_panel.go — Settings tab with mode-aware fields.
//
// The panel shows a different set of controls depending on the currently
// selected chat mode:
//
//   Board  → Temperature, Timeout
//   Events → Temperature, Timeout, Max Tokens, Top Hits
//
// Changes apply immediately to chatSettings — no apply button, no persistence.
// All values are temporary and reset when the window is closed.

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// chatSettings holds the tunable parameters for both chat modes.
//
// It is safe for concurrent access — the UI reads the values from a
// background goroutine when sending a message. All mutations go through
// the setter methods which take the lock.
type chatSettings struct {
	mu sync.RWMutex

	temperature float32       // 0.0 – 2.0
	timeout     time.Duration // LLM + search round-trip cap
	maxTokens   int           // response-length cap (event mode only)
	topHits     int           // number of vector hits fed to the LLM
}

// newChatSettings returns settings pre-populated with sensible defaults.
//
// Temperature 0.2 is low enough for factual RAG answers but not so
// deterministic that the model loses natural phrasing. 5-minute timeout
// matches the existing hard-coded value in window.go.
func newChatSettings() *chatSettings {
	return &chatSettings{
		temperature: 0.2,
		timeout:     5 * time.Minute,
		maxTokens:   4096,
		topHits:     10,
	}
}

// Temperature returns the currently configured sampling temperature.
func (s *chatSettings) Temperature() float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.temperature
}

// Timeout returns the currently configured LLM round-trip timeout.
func (s *chatSettings) Timeout() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeout
}

// MaxTokens returns the currently configured response length cap.
func (s *chatSettings) MaxTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxTokens
}

// TopHits returns the currently configured vector-search hit count.
func (s *chatSettings) TopHits() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.topHits
}

// settingsPanel is the Fyne widget that renders the Settings tab.
//
// The body container is rebuilt on every mode change so the user only
// sees the fields that are relevant for the current mode.
type settingsPanel struct {
	app      fyne.App
	settings *chatSettings
	body     *fyne.Container
	root     fyne.CanvasObject
	mode     chatMode
}

// newSettingsPanel creates a panel bound to the given settings struct.
// It starts in board mode — call SetMode(modeEvents) afterwards to switch.
func newSettingsPanel(app fyne.App, settings *chatSettings) *settingsPanel {
	p := &settingsPanel{
		app:      app,
		settings: settings,
		body:     container.NewVBox(),
		mode:     modeBoard,
	}

	resetBtn := widget.NewButton("Reset to defaults", func() {
		def := newChatSettings()
		p.settings.mu.Lock()
		p.settings.temperature = def.temperature
		p.settings.timeout = def.timeout
		p.settings.maxTokens = def.maxTokens
		p.settings.topHits = def.topHits
		p.settings.mu.Unlock()
		p.rebuild()
	})

	// Debug button opens a window with all recorded tool calls as a
	// selectable, copyable text document — the go-to place when a user
	// wants to share what the agent actually did with an admin.
	debugBtn := widget.NewButton("Debug log", func() {
		OpenDebugWindow(p.app)
	})

	p.root = container.NewBorder(
		widget.NewLabelWithStyle("Chat settings (temporary — reset on close)",
			fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(debugBtn, resetBtn),
		nil, nil,
		container.NewPadded(p.body),
	)

	p.rebuild()
	return p
}

// canvasObject returns the panel's root widget for embedding.
func (p *settingsPanel) canvasObject() fyne.CanvasObject {
	return p.root
}

// SetMode switches the panel between board and events layouts.
func (p *settingsPanel) SetMode(m chatMode) {
	if p.mode == m {
		return
	}
	p.mode = m
	p.rebuild()
}

// rebuild replaces the body with a fresh set of controls for the current mode.
func (p *settingsPanel) rebuild() {
	p.body.RemoveAll()

	// Always shown: temperature + timeout.
	p.body.Add(p.buildTemperatureRow())
	p.body.Add(p.buildTimeoutRow())

	// Event-mode extras.
	if p.mode == modeEvents {
		p.body.Add(p.buildMaxTokensRow())
		p.body.Add(p.buildTopHitsRow())
	}

	p.body.Refresh()
}

// ── Row builders ──────────────────────────────────────────────────────────────

// buildTemperatureRow renders a slider (0.0–2.0, step 0.1) with a live label.
func (p *settingsPanel) buildTemperatureRow() fyne.CanvasObject {
	current := p.settings.Temperature()

	valueLbl := widget.NewLabel(fmt.Sprintf("%.1f", current))
	slider := widget.NewSlider(0, 2)
	slider.Step = 0.1
	slider.Value = float64(current)
	slider.OnChanged = func(v float64) {
		p.settings.mu.Lock()
		p.settings.temperature = float32(v)
		p.settings.mu.Unlock()
		valueLbl.SetText(fmt.Sprintf("%.1f", v))
	}

	return labeledRow(
		"Temperature",
		"Lower = more factual, higher = more creative. RAG works best at 0.2.",
		container.NewBorder(nil, nil, nil, valueLbl, slider),
	)
}

// buildTimeoutRow renders a read-only entry with preset buttons.
// We avoid a slider here because the useful range spans three orders of magnitude.
func (p *settingsPanel) buildTimeoutRow() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetText(p.settings.Timeout().String())
	entry.OnChanged = func(s string) {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return // keep previous value until input is valid
		}
		p.settings.mu.Lock()
		p.settings.timeout = d
		p.settings.mu.Unlock()
	}

	return labeledRow(
		"Timeout",
		"How long to wait for the LLM. Format: 30s, 5m, 10m.",
		entry,
	)
}

// buildMaxTokensRow renders an integer entry for the response length cap.
func (p *settingsPanel) buildMaxTokensRow() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetText(strconv.Itoa(p.settings.MaxTokens()))
	entry.OnChanged = func(s string) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 256 || n > 16384 {
			return
		}
		p.settings.mu.Lock()
		p.settings.maxTokens = n
		p.settings.mu.Unlock()
	}

	return labeledRow(
		"Max tokens",
		"Upper bound on response length. 4096 fits most answers; raise for very long reports.",
		entry,
	)
}

// buildTopHitsRow renders an integer entry for the vector-search hit count.
func (p *settingsPanel) buildTopHitsRow() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetText(strconv.Itoa(p.settings.TopHits()))
	entry.OnChanged = func(s string) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 50 {
			return
		}
		p.settings.mu.Lock()
		p.settings.topHits = n
		p.settings.mu.Unlock()
	}

	return labeledRow(
		"Top hits",
		"Number of events fed to the LLM. More = richer context but bigger prompt.",
		entry,
	)
}

// labeledRow wraps a single control with a title and a help line underneath.
func labeledRow(title, help string, ctrl fyne.CanvasObject) fyne.CanvasObject {
	titleLbl := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	helpLbl := widget.NewLabel(help)
	helpLbl.TextStyle = fyne.TextStyle{Italic: true}
	helpLbl.Wrapping = fyne.TextWrapWord

	return container.NewPadded(container.NewVBox(
		titleLbl,
		ctrl,
		helpLbl,
	))
}
