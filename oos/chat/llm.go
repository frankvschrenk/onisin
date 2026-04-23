package chat

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

type ToolParameters struct {
	Type       string              `json:"type"`
	Properties map[string]ToolProp `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type ToolProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON als String
}

type ChunkHandler func(token string)

type LLMClient interface {
	Chat(ctx context.Context, messages []Message, tools []Tool, onChunk ChunkHandler) (string, []ToolCall, error)

	ModelName() string
}
