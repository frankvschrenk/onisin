package helper

import "encoding/json"

func parseJSON(raw string, v any) error {
	return json.Unmarshal([]byte(raw), v)
}
