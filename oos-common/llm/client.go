package llm

// client.go — LLM connection helpers.
//
// Speaks to any OpenAI-compatible endpoint:
//   - Ollama              (http://localhost:11434/v1)  ← default
//   - vLLM / Ray cluster  (configurable via oos.toml)
//   - Any remote provider with an OpenAI-compatible API
//
// The package exposes a small set of package-level variables that act as
// the process-wide LLM configuration. Both oos (client) and ooso (AI-assisted
// DSL editor) read from the same variables so that changing the endpoint
// once takes effect everywhere.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// URL is the base URL of the OpenAI-compatible LLM endpoint.
// Default points to Ollama running locally.
var URL = "http://localhost:11434"

// APIKey is the API key sent as Bearer token.
// Leave empty for local models that do not require authentication.
var APIKey = ""

// ChatModel is the model name to use for chat completions.
// When empty, FirstChatModel queries /v1/models and picks the first
// non-embedding model.
var ChatModel = ""

// SchemaStrategy controls how the OOS schema is injected into the AI prompt.
//
//	rag     — no schema in prompt; model calls oos_schema_search (fast, default)
//	compact — context names only in prompt; details via oos_schema_search
//	full    — all chunks in prompt (best for large models / vLLM clusters)
//
// Default: compact
var SchemaStrategy = "compact"

// Models queries the /v1/models endpoint and returns all available model IDs.
func Models() ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest(http.MethodGet, URL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM endpoint not reachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("models response invalid: %w", err)
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// FirstChatModel returns ChatModel if set, otherwise queries /v1/models
// and returns the first model that is not an embedding model.
func FirstChatModel() (string, error) {
	if ChatModel != "" {
		return ChatModel, nil
	}
	models, err := Models()
	if err != nil {
		return "", err
	}
	for _, m := range models {
		if !IsEmbeddingModel(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("no chat model available at %s", URL)
}

// IsEmbeddingModel returns true when the model name suggests it is an
// embedding-only model that cannot be used for chat completions.
//
// Exported so that UI code (model pickers that want to hide embedding
// models from the chat dropdown) can reuse the same classification.
func IsEmbeddingModel(name string) bool {
	keywords := []string{"embedding", "embed", "e5-", "bge-", "gte-", "granite-embedding"}
	lower := strings.ToLower(name)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
