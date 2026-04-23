package aiassist

// event_mode.go — Mode switch header with mapping and stream-id controls.
//
// Layout (one row above the main split):
//
//   [(●) Board (GraphQL)  ( ) Events & Vectors]   Context: [police ▼]   Stream-ID: [________]
//
// The mapping dropdown and stream-id entry are only visible in event mode.
// Switching modes fires onModeChange(); the window uses that callback to
// swap the body of the split between board and event panels.

import (
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos/helper"
)

// chatMode is the active mode of the AI assistant window.
type chatMode string

const (
	modeBoard  chatMode = "board"
	modeEvents chatMode = "events"
)

// modeHeader is the top bar that lets the user switch modes and pick an
// event mapping + optional stream ID. It also hosts a permanently-visible
// model selector that applies to both board and event modes.
type modeHeader struct {
	radio      *widget.RadioGroup
	modelSel   *widget.Select
	modelLbl   *widget.Label
	modelRow   *fyne.Container
	mapping    *widget.Select
	streamID   *widget.Entry
	ctxLabel   *widget.Label
	streamLbl  *widget.Label
	streamRow  *fyne.Container
	mappingRow *fyne.Container
	root       fyne.CanvasObject

	mode          chatMode
	onModeChange  func(chatMode)
	onModelChange func(string)
}

// newModeHeader creates the header in Board mode (default).
// onModeChange is invoked whenever the user flips the radio.
// onModelChange is invoked whenever the user picks a different model.
func newModeHeader(onModeChange func(chatMode), onModelChange func(string)) *modeHeader {
	h := &modeHeader{
		mode:          modeBoard,
		onModeChange:  onModeChange,
		onModelChange: onModelChange,
	}

	// Radio — two options, horizontal.
	h.radio = widget.NewRadioGroup(
		[]string{"Board (GraphQL)", "Events & Vectors"},
		func(choice string) {
			switch choice {
			case "Board (GraphQL)":
				h.setMode(modeBoard)
			case "Events & Vectors":
				h.setMode(modeEvents)
			}
		},
	)
	h.radio.Horizontal = true
	h.radio.SetSelected("Board (GraphQL)")

	// Model dropdown (permanent, applies to both modes).
	h.modelLbl = widget.NewLabel("Model:")
	h.modelSel = widget.NewSelect([]string{}, func(picked string) {
		if h.onModelChange != nil && picked != "" {
			h.onModelChange(picked)
		}
	})
	h.modelSel.PlaceHolder = "(loading...)"
	modelWrap := container.NewGridWrap(fyne.NewSize(200, 36), h.modelSel)
	h.modelRow = container.NewBorder(nil, nil, h.modelLbl, nil, modelWrap)

	// Mapping dropdown.
	h.ctxLabel = widget.NewLabel("Context:")
	h.mapping = widget.NewSelect([]string{}, func(_ string) {})
	h.mapping.PlaceHolder = "(loading...)"
	mappingWrap := container.NewGridWrap(fyne.NewSize(180, 36), h.mapping)
	h.mappingRow = container.NewBorder(nil, nil, h.ctxLabel, nil, mappingWrap)

	// Stream ID text field.
	h.streamLbl = widget.NewLabel("Stream-ID:")
	h.streamID = widget.NewEntry()
	h.streamID.SetPlaceHolder("(empty = all streams)")
	streamWrap := container.NewGridWrap(fyne.NewSize(240, 36), h.streamID)
	h.streamRow = container.NewBorder(nil, nil, h.streamLbl, nil, streamWrap)

	// Both extra controls start hidden (Board is the default mode).
	h.mappingRow.Hide()
	h.streamRow.Hide()

	// Assemble the row.
	h.root = container.NewBorder(
		nil, nil,
		h.radio,
		nil,
		container.NewHBox(h.modelRow, h.mappingRow, h.streamRow),
	)

	// Populate the model dropdown in the background — this talks to
	// the LLM endpoint so we don't want to block window construction.
	go h.loadModels()

	return h
}

// canvasObject returns the header widget ready for embedding.
func (h *modeHeader) canvasObject() fyne.CanvasObject {
	return h.root
}

// Mode returns the currently selected mode.
func (h *modeHeader) Mode() chatMode { return h.mode }

// Mapping returns the currently selected event mapping, or "" if none.
func (h *modeHeader) Mapping() string { return h.mapping.Selected }

// Model returns the currently selected model name, or "" if none.
func (h *modeHeader) Model() string { return h.modelSel.Selected }

// StreamID returns the current stream-id filter, or "" if none.
func (h *modeHeader) StreamID() string { return h.streamID.Text }

// setMode updates the mode, toggles visibility of the extra controls
// and forwards the change to the owner.
func (h *modeHeader) setMode(m chatMode) {
	if h.mode == m {
		return
	}
	h.mode = m
	if m == modeEvents {
		h.mappingRow.Show()
		h.streamRow.Show()
		go h.loadMappings()
	} else {
		h.mappingRow.Hide()
		h.streamRow.Hide()
	}
	h.root.Refresh()
	if h.onModeChange != nil {
		h.onModeChange(m)
	}
}

// loadModels queries the LLM endpoint for the available chat models and
// populates the model dropdown. The user's configured default is pre-selected
// when present, otherwise the first returned model wins.
func (h *modeHeader) loadModels() {
	models, err := helper.LLMModels()
	if err != nil {
		log.Printf("[aiassist] fetch models: %v", err)
		fyne.Do(func() {
			h.modelSel.PlaceHolder = "(not available)"
			h.modelSel.Refresh()
		})
		return
	}

	// Drop embedding-only models — the chat doesn't use them.
	chatModels := make([]string, 0, len(models))
	for _, m := range models {
		if !isChatLikeName(m) {
			continue
		}
		chatModels = append(chatModels, m)
	}

	fyne.Do(func() {
		h.modelSel.Options = chatModels
		switch {
		case len(chatModels) == 0:
			h.modelSel.PlaceHolder = "(none found)"
		case helper.LLMChatModel != "" && containsString(chatModels, helper.LLMChatModel):
			h.modelSel.SetSelected(helper.LLMChatModel)
		default:
			h.modelSel.SetSelected(chatModels[0])
		}
		h.modelSel.Refresh()
	})
}

// isChatLikeName returns false for model names that look like embedding-only
// models. Mirrors helper.isEmbeddingModel but we can't reach that one from
// here — keep the short list in sync when new providers appear.
func isChatLikeName(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range []string{"embedding", "embed", "e5-", "bge-", "gte-"} {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}

// containsString reports whether s is in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// loadMappings fetches event mappings from oosp and populates the dropdown.
// Runs in a goroutine so the UI doesn't block.
func (h *modeHeader) loadMappings() {
	mappings, err := FetchEventMappings()
	if err != nil {
		log.Printf("[aiassist] fetch event mappings: %v", err)
		fyne.Do(func() {
			h.mapping.PlaceHolder = "(not available)"
			h.mapping.Refresh()
		})
		return
	}

	names := make([]string, 0, len(mappings))
	for _, m := range mappings {
		if m.Enabled {
			names = append(names, m.Name)
		}
	}

	fyne.Do(func() {
		h.mapping.Options = names
		if len(names) > 0 {
			h.mapping.SetSelected(names[0])
		} else {
			h.mapping.PlaceHolder = "(none configured)"
		}
		h.mapping.Refresh()
	})
}
