package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIClient struct {
	baseURL string
	apiKey  string 
	model   string
}

func NewOpenAIClient(baseURL, apiKey, model string) *OpenAIClient {
	return &OpenAIClient{baseURL: baseURL, apiKey: apiKey, model: model}
}

func (c *OpenAIClient) ModelName() string { return c.model }

type openAIRequest struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Tools    []openAITool `json:"tools,omitempty"`
	Stream   bool         `json:"stream"`
}

type openAITool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) Chat(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	onChunk ChunkHandler,
) (string, []ToolCall, error) {
	openAITools := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		openAITools = append(openAITools, openAITool{Type: t.Type, Function: t.Function})
	}

	body, err := json.Marshal(openAIRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    openAITools,
		Stream:   false,
	})
	if err != nil {
		return "", nil, fmt.Errorf("openai: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("openai: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("llm nicht erreichbar (%s): %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("openai: body lesen: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct{ Message string `json:"message"` } `json:"error"`
		}
		if json.Unmarshal(rawBody, &errResp) == nil && errResp.Error.Message != "" {
			return "", nil, fmt.Errorf("llm: %s", errResp.Error.Message)
		}
		return "", nil, fmt.Errorf("llm: HTTP %d", resp.StatusCode)
	}

	var result openAIResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return "", nil, fmt.Errorf("openai: antwort parsen: %w", err)
	}
	if result.Error != nil {
		return "", nil, fmt.Errorf("llm: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("llm: keine Antwort erhalten")
	}

	choice := result.Choices[0].Message

	calls := make([]ToolCall, 0, len(choice.ToolCalls))
	for _, tc := range choice.ToolCalls {
		calls = append(calls, ToolCall{
			Function: ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return choice.Content, calls, nil
}
