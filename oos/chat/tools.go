package chat

func OOSTools() []Tool {
	return []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "oos_query",
				Description: "Lädt Daten per GraphQL und zeigt sie in einem Board-Fenster an. Immer als ersten Schritt verwenden wenn Daten angezeigt werden sollen.",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProp{
						"context": {Type: "string", Description: "Context-Name, z.B. person_detail oder person_list"},
						"query":   {Type: "string", Description: "GraphQL Query, z.B. { person(id: 1) { id firstname lastname } }"},
					},
					Required: []string{"context", "query"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "oos_render",
				Description: "Rendert JSON-Daten mit einer DSL-Screen-Definition in einem Board-Fenster. Für dynamische Ansichten die kein vordefiniertes DSL haben.",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProp{
						"context": {Type: "string", Description: "Screen-ID, z.B. analyse_ergebnis"},
						"dsl":     {Type: "string", Description: "XML DSL-Screen-Definition"},
						"json":    {Type: "string", Description: "JSON-Daten die gerendert werden sollen"},
					},
					Required: []string{"context", "dsl", "json"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "oos_stream_append",
				Description: "Schreibt einen Event append-only in die Stream-Datenbank (KurrentDB). Für Ereignisse die dauerhaft protokolliert werden sollen.",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProp{
						"stream":     {Type: "string", Description: "Stream-Name, z.B. fall-2024-0042"},
						"event_type": {Type: "string", Description: "Event-Typ, z.B. ZeugenAussageAufgenommen"},
						"data":       {Type: "string", Description: "JSON-Daten des Events"},
						"text":       {Type: "string", Description: "Freitext der eingebettet wird (für RAG)"},
					},
					Required: []string{"stream", "event_type", "data"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "oos_vector_search",
				Description: "Sucht semantisch ähnliche Einträge in der Vector-Datenbank (Qdrant). Für RAG-Abfragen über gespeicherte Events.",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProp{
						"query":      {Type: "string", Description: "Suchanfrage in natürlicher Sprache"},
						"collection": {Type: "string", Description: "Collection-Name, z.B. oos_events"},
						"filter":     {Type: "string", Description: "Optionaler JSON-Filter, z.B. {\"stream\": \"fall-2024-0042\"}"},
						"n":          {Type: "string", Description: "Anzahl Treffer (default 5)"},
					},
					Required: []string{"query", "collection"},
				},
			},
		},
	}
}
