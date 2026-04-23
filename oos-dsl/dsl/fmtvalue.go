// fmtvalue.go — Konvertierung von JSON-Werten zu Anzeigestrings.
// Ausgelagert aus builder.go damit es unabhängig testbar und erweiterbar ist.
package dsl

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// fmtValue wandelt einen JSON-Wert in einen lesbaren String um.
// Floats werden niemals als scientific notation ausgegeben.
// Booleans werden als "true"/"false" gespeichert (nicht "ja"/"nein")
// damit check.SetChecked(state.Get(bp) == "true") korrekt funktioniert.
func fmtValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// Ganzzahl ohne Dezimalstellen
		if val == float64(int64(val)) && val < 1e15 {
			return strconv.FormatInt(int64(val), 10)
		}
		// Dezimalzahl — immer mit 2 Stellen, niemals scientific notation
		return strconv.FormatFloat(val, 'f', 2, 64)
	case bool:
		// "true"/"false" — nicht "ja"/"nein", damit Datenbinding funktioniert
		if val {
			return "true"
		}
		return "false"
	case nil:
		return ""
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// fmtValueDisplay gibt einen boolean als lesbares Deutsch zurück.
// Wird für Labels und reine Anzeige genutzt (nicht für Datenbinding).
func fmtValueDisplay(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "Ja"
		}
		return "Nein"
	}
	return fmtValue(v)
}
