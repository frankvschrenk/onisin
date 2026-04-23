package chat

// handler.go — tool call dispatcher for the legacy chat package.
//
// ExecuteToolCall maps a tool name to the corresponding function in the
// tools/ package. This keeps the old chat/ package working while the
// migration to the eino-based aiassist/ package is in progress.

import (
	"encoding/json"
	"fmt"
	"log"

	oostools "onisin.com/oos/tools"
)

// ExecuteToolCall dispatches a tool call by name and returns the result string.
func ExecuteToolCall(call ToolCall) string {
	name := call.Function.Name
	log.Printf("[chat] tool call: %s", name)

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("error: invalid arguments: %v", err)
	}

	result, err := dispatchTool(name, args)
	if err != nil {
		log.Printf("[chat] tool %s error: %v", name, err)
		return fmt.Sprintf("error in %s: %v", name, err)
	}
	return result
}

// dispatchTool routes to the correct tools/ function by tool name.
func dispatchTool(name string, args map[string]any) (string, error) {
	str := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	switch name {
	case "oos_query":
		return oostools.Query(str("context"), str("query"))
	case "oos_render":
		return oostools.Render(str("context"), str("json"))
	case "oos_ui_change_required":
		return oostools.UIChange(str("context"), str("json"))
	case "oos_ui_save":
		return oostools.UISave()
	case "oos_new":
		return oostools.UINew(str("context"))
	case "oos_delete":
		return oostools.Delete(str("context"), str("id"))
	case "oos_stream_append":
		return oostools.OOSPStreamAppend(str("stream"), str("event_type"), str("data"))
	case "oos_vector_search":
		return oostools.OOSPVectorSearch(str("collection"), str("vector"), str("filter"), str("n"))
	case "oos_system_status":
		return oostools.Status(), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
