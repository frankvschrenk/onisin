package aiassist

// model.go — eino ChatModel factory.
//
// NewChatModel creates an OpenAI-compatible chat model via eino.
// Works with any endpoint that implements POST /v1/chat/completions:
//   - Ollama  (http://localhost:11434)
//   - vLLM    (configurable)
//   - Ray     (configurable)
//   - Any remote provider with an OpenAI-compatible API

import (
	"context"
	"fmt"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"onisin.com/oos-common/llm"
)

// newChatModel creates an eino ToolCallingChatModel using the default model
// resolved via llm.FirstChatModel (preferred user setting or first
// non-embedding model discovered at the endpoint).
func newChatModel(ctx context.Context) (model.ToolCallingChatModel, string, error) {
	modelName, err := llm.FirstChatModel()
	if err != nil {
		return nil, "", fmt.Errorf("no chat model available: %w", err)
	}
	return newChatModelWithName(ctx, modelName)
}

// newChatModelWithName creates an eino ToolCallingChatModel for a specific
// model name. Used when the user switches models at runtime via the UI.
func newChatModelWithName(ctx context.Context, modelName string) (model.ToolCallingChatModel, string, error) {
	if modelName == "" {
		return newChatModel(ctx)
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

// buildBaseURL returns the correct /v1 base URL for the OpenAI-compatible API.
// If the configured llm.URL already ends with /v1 it is used as-is,
// otherwise /v1 is appended. This handles both:
//   - Ollama:  http://localhost:11434      → http://localhost:11434/v1
//   - vLLM:    http://host:8000/v1         → http://host:8000/v1
func buildBaseURL() string {
	u := strings.TrimRight(llm.URL, "/")
	if strings.HasSuffix(u, "/v1") {
		return u
	}
	return u + "/v1"
}
