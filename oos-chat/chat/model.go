package chat

// model.go — eino ChatModel factory shared across modes.
//
// Builds an OpenAI-compatible chat model against whatever endpoint
// llm.URL points at. Used by every Session regardless of mode —
// modes only differ in tools and system prompt, not in transport.

import (
	"context"
	"fmt"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"onisin.com/oos-common/llm"
)

// newChatModel resolves and constructs a ToolCallingChatModel against
// the configured endpoint. Pass an empty modelName to use
// llm.FirstChatModel (preferred user setting or first non-embedding
// model discovered at the endpoint).
func newChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, string, error) {
	if modelName == "" {
		name, err := llm.FirstChatModel()
		if err != nil {
			return nil, "", fmt.Errorf("no chat model available: %w", err)
		}
		modelName = name
	}

	apiKey := llm.APIKey
	if apiKey == "" {
		// Ollama and most local endpoints accept any non-empty key.
		apiKey = "ollama"
	}

	cm, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: buildBaseURL(),
		APIKey:  apiKey,
		Model:   modelName,
	})
	if err != nil {
		return nil, "", fmt.Errorf("eino chat model: %w", err)
	}
	return cm, modelName, nil
}

// buildBaseURL normalises the configured llm.URL to an OpenAI-style
// /v1 base. Handles both Ollama (http://localhost:11434) and vLLM
// (http://host:8000/v1) by checking whether /v1 is already present.
func buildBaseURL() string {
	u := strings.TrimRight(llm.URL, "/")
	if strings.HasSuffix(u, "/v1") {
		return u
	}
	return u + "/v1"
}
