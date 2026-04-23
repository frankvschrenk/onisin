// state.go — Live-Werte aller gebundenen Felder und Laufzeit-Options.
package dsl

import (
	"encoding/json"
	"fmt"
	"sync"
)

// OptionEntry ist ein einzelner Eintrag einer Auswahlliste.
type OptionEntry struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// State ist ein thread-sicherer Speicher für Feldwerte und Options.
//
// Feldwerte werden als dot-separated Pfade gespeichert:
//
//	person.firstname = "Frank"
//	person.address.city = "München"
//
// Options werden als Schlüssel→[]OptionEntry gespeichert:
//
//	"countries" → [{value:"de", label:"Deutschland"}, ...]
type State struct {
	mu      sync.RWMutex
	values  map[string]string
	options map[string][]OptionEntry
}

func NewState() *State {
	return &State{
		values:  make(map[string]string),
		options: make(map[string][]OptionEntry),
	}
}

func (s *State) Set(bindPath, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[bindPath] = value
}

func (s *State) Get(bindPath string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[bindPath]
}

func (s *State) GetOptions(key string) []OptionEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.options[key]
}

func (s *State) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

func (s *State) SnapshotOptions() map[string][]OptionEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]OptionEntry, len(s.options))
	for k, v := range s.options {
		out[k] = v
	}
	return out
}

// LoadJSON lädt Daten + Options aus dem oosp-Envelope.
//
// Erwartetes Format (neu):
//
//	{
//	  "content": { "person": { "firstname": "Frank", "country": "de" } },
//	  "meta":    { "countries": [{"value":"de","label":"Deutschland"}] }
//	}
//
// Fallback (alt — flaches JSON):
//
//	{ "firstname": "Frank", "country": "de" }
//
// Wichtig: Content und Meta werden unabhaengig voneinander geladen.
// Beim Oeffnen eines leeren Entity-Screens ("Neu"-Pfad) ist Content leer
// oder null, Meta aber vorhanden — die Dropdowns muessen trotzdem befuellt
// werden. Eine strikte "content muss map sein" Pruefung wuerde in genau
// diesem Fall die Metas mit stillschweigend verwerfen.
func (s *State) LoadJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("state.LoadJSON: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// New envelope format: content + meta as independent branches.
	// The presence of either marker signals the envelope form; we load
	// whichever branch is present without requiring the other.
	_, hasContentKey := raw["content"]
	_, hasMetaKey := raw["meta"]
	if hasContentKey || hasMetaKey {
		if contentSection, ok := raw["content"].(map[string]any); ok {
			flattenInto(s.values, "", contentSection)
		}
		if metaSection, ok := raw["meta"].(map[string]any); ok {
			loadOptions(s.options, metaSection)
		}
		return nil
	}

	// Transitional format: data + options (kept for compatibility with
	// older oosp responses that haven't been migrated to the envelope).
	_, hasDataKey := raw["data"]
	_, hasOptionsKey := raw["options"]
	if hasDataKey || hasOptionsKey {
		if dataSection, ok := raw["data"].(map[string]any); ok {
			flattenInto(s.values, "", dataSection)
		}
		if optSection, ok := raw["options"].(map[string]any); ok {
			loadOptions(s.options, optSection)
		}
		return nil
	}

	// Legacy fallback: flat JSON. No meta in this shape — dropdowns would
	// have to come from inline <option> children only.
	flattenInto(s.values, "", raw)
	return nil
}

// flattenInto expandiert eine nested map in dot-separated Pfade.
// Arrays werden als JSON-String gespeichert (für Tabellen-Bindings).
func flattenInto(out map[string]string, prefix string, v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flattenInto(out, key, child)
		}
	case []any:
		b, _ := json.Marshal(val)
		out[prefix] = string(b)
	default:
		out[prefix] = fmtValue(val)
	}
}

// loadOptions füllt die Options-Map aus dem meta-Abschnitt.
// Unterstützt:
//   - [{value:"de", label:"Deutschland"}, ...]  → OptionEntry mit Value+Label
//   - ["München", "Berlin", ...]                → OptionEntry mit Value=Label
func loadOptions(out map[string][]OptionEntry, metaSection map[string]any) {
	for key, raw := range metaSection {
		arr, ok := raw.([]any)
		if !ok {
			continue
		}
		entries := make([]OptionEntry, 0, len(arr))
		for _, item := range arr {
			switch v := item.(type) {
			case map[string]any:
				entry := OptionEntry{
					Value: fmtValue(v["value"]),
					Label: fmtValue(v["label"]),
				}
				if entry.Label == "" {
					entry.Label = entry.Value
				}
				entries = append(entries, entry)
			case string:
				entries = append(entries, OptionEntry{Value: v, Label: v})
			}
		}
		out[key] = entries
	}
}
