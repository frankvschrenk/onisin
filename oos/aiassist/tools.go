package aiassist

// tools.go — eino tool definitions for the OOS AI assistant.
//
// Each tool wraps a function from the tools/ package.
// The eino framework calls these functions when the LLM requests a tool.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	oostools "onisin.com/oos/tools"
)

// oosTool implements tool.InvokableTool for a single OOS action.
type oosTool struct {
	info *schema.ToolInfo
	fn   func(args map[string]any) (string, error)
}

func (t *oosTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *oosTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return "", fmt.Errorf("tool %s: invalid args: %w", t.info.Name, err)
	}
	return t.fn(m)
}

// str extracts a string value from the argument map.
func str(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// buildTools returns all OOS tools as eino BaseTool instances.
func buildTools() []tool.BaseTool {
	return []tool.BaseTool{
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_schema_search",
				Desc: "Search the OOS schema for context definitions relevant to the user's request. " +
					"Always call this FIRST before oos_query to find the correct context name, " +
					"GraphQL fields and query structure.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"query": {Type: schema.String, Desc: "What the user wants to see or do, e.g. 'show all persons' or 'notes for a person'", Required: true},
					"n":     {Type: schema.String, Desc: "Number of schema chunks to return (default 3)"},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				n := 3
				if ns := str(m, "n"); ns != "" {
					fmt.Sscanf(ns, "%d", &n) //nolint:errcheck
				}
				return oostools.SchemaSearch(str(m, "query"), n)
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_query",
				Desc: "Load data via GraphQL and render it in the board. Always use this first when the user wants to see data.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"context": {Type: schema.String, Desc: "Context name, e.g. person_list or person_detail", Required: true},
					"query":   {Type: schema.String, Desc: "GraphQL query, e.g. { person_list { id firstname lastname } }", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.Query(str(m, "context"), str(m, "query"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_ui_change_required",
				Desc: "Write AI changes as a preview into the board. No database write. User must confirm before saving.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"context": {Type: schema.String, Desc: "Context name", Required: true},
					"json":    {Type: schema.String, Desc: "Changed fields as JSON object", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.UIChange(str(m, "context"), str(m, "json"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_ui_save",
				Desc: "Persist board data to the database. Only call after explicit user confirmation.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
			},
			fn: func(_ map[string]any) (string, error) {
				return oostools.UISave()
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_new",
				Desc: "Open an empty input screen for a context so the user can fill in a new record.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"context": {Type: schema.String, Desc: "Context name", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.UINew(str(m, "context"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_delete",
				Desc: "Permanently delete a record. Only call after explicit user confirmation.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"context": {Type: schema.String, Desc: "Context name", Required: true},
					"id":      {Type: schema.String, Desc: "Record ID", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.Delete(str(m, "context"), str(m, "id"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_render",
				Desc: "Render arbitrary JSON data into the board without a database query.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"context": {Type: schema.String, Desc: "Screen ID", Required: true},
					"json":    {Type: schema.String, Desc: "JSON data to render", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.Render(str(m, "context"), str(m, "json"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_stream_append",
				Desc: "Append an event to the event stream (append-only, persisted).",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"stream":     {Type: schema.String, Desc: "Stream name, e.g. fall-2024-0042", Required: true},
					"event_type": {Type: schema.String, Desc: "Event type name", Required: true},
					"data":       {Type: schema.String, Desc: "Event payload as JSON", Required: true},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.OOSPStreamAppend(str(m, "stream"), str(m, "event_type"), str(m, "data"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_vector_search",
				Desc: "Semantic similarity search over stored events.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"collection": {Type: schema.String, Desc: "Collection name", Required: true},
					"vector":     {Type: schema.String, Desc: "Query vector as JSON array", Required: true},
					"filter":     {Type: schema.String, Desc: "Optional JSON filter"},
					"n":          {Type: schema.String, Desc: "Number of results (default 5)"},
				}),
			},
			fn: func(m map[string]any) (string, error) {
				return oostools.OOSPVectorSearch(str(m, "collection"), str(m, "vector"), str(m, "filter"), str(m, "n"))
			},
		},
		&oosTool{
			info: &schema.ToolInfo{
				Name: "oos_system_status",
				Desc: "Return current OOS system status and available queries.",
				ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
			},
			fn: func(_ map[string]any) (string, error) {
				return oostools.Status(), nil
			},
		},
	}
}
