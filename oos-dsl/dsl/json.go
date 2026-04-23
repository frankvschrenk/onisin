// json.go converts the current State into a structured JSON event payload.
package dsl

import (
	"encoding/json"
	"strings"
)

// BuildEvent gibt die Formulardaten als flaches JSON zurück.
// dot-separated Keys ("person.id", "person.age") werden aufgelöst:
// der erste Prefix-Level wird entfernt — "person.id" → "id".
// oosp bekommt genau die flachen Felder die er für die Mutation braucht.
func BuildEvent(screenID, action string, state *State) ([]byte, error) {
	snap := state.Snapshot()
	out := make(map[string]any, len(snap))
	for k, v := range snap {
		key := k
		if idx := strings.Index(k, "."); idx >= 0 {
			key = k[idx+1:]
		}
		out[key] = v
	}
	return json.MarshalIndent(out, "", "  ")
}

// expandPaths wandelt {"user.address.city": "München"} in
// {"user": {"address": {"city": "München"}}} um.
// Werte die mit "[" beginnen werden als json.RawMessage eingebettet.
func expandPaths(flat map[string]string) map[string]any {
	root := make(map[string]any)
	for path, value := range flat {
		parts := strings.Split(path, ".")
		var val any
		if looksLikeArray(value) {
			val = json.RawMessage(value)
		} else {
			val = value
		}
		setNested(root, parts, val)
	}
	return root
}

func looksLikeArray(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")
}

func setNested(m map[string]any, parts []string, value any) {
	if len(parts) == 1 {
		m[parts[0]] = value
		return
	}
	child, ok := m[parts[0]].(map[string]any)
	if !ok {
		child = make(map[string]any)
		m[parts[0]] = child
	}
	setNested(child, parts[1:], value)
}
