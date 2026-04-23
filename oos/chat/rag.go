package chat

// rag.go — Gemeinsame RAG Typen fuer oos/chat.

// Quelle ist ein Treffer aus der RAG-Suche (oosai /rag).
type Quelle struct {
	EventID   string         `json:"event_id"`
	StreamID  string         `json:"stream_id"`
	EventType string         `json:"event_type"`
	Score     float64        `json:"score"`
	Data      map[string]any `json:"data"`
}
