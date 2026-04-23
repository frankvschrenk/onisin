package helper

// llm.go — LLM connection helpers.
//
// Speaks to any OpenAI-compatible endpoint:
//   - Ollama              (http://localhost:11434/v1)  ← default
//   - vLLM / Ray cluster  (configurable via oos.toml)
//   - Any remote provider with an OpenAI-compatible API

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMUrl is the base URL of the OpenAI-compatible LLM endpoint.
// Default points to Ollama running locally.
var LLMUrl = "http://localhost:11434"

// LLMApiKey is the API key sent as Bearer token.
// Leave empty for local models that do not require authentication.
var LLMApiKey = ""

// LLMChatModel is the model name to use for chat completions.
// When empty, LLMFirstChatModel queries /v1/models and picks the first
// non-embedding model.
var LLMChatModel = ""

// LLMSchemaStrategy controls how the OOS schema is injected into the AI prompt.
//
//   rag     — no schema in prompt; model calls oos_schema_search (fast, default)
//   compact — context names only in prompt; details via oos_schema_search
//   full    — all chunks in prompt (best for large models / vLLM clusters)
//
// Default: compact
var LLMSchemaStrategy = "compact"

// LLMModels queries the /v1/models endpoint and returns all available model IDs.
func LLMModels() ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest(http.MethodGet, LLMUrl+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if LLMApiKey != "" {
		req.Header.Set("Authorization", "Bearer "+LLMApiKey)
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

// LLMFirstChatModel returns LLMChatModel if set, otherwise queries /v1/models
// and returns the first model that is not an embedding model.
func LLMFirstChatModel() (string, error) {
	if LLMChatModel != "" {
		return LLMChatModel, nil
	}
	models, err := LLMModels()
	if err != nil {
		return "", err
	}
	for _, m := range models {
		if !isEmbeddingModel(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("no chat model available at %s", LLMUrl)
}

// isEmbeddingModel returns true when the model name suggests it is an
// embedding-only model that cannot be used for chat completions.
func isEmbeddingModel(name string) bool {
	keywords := []string{"embedding", "embed", "e5-", "bge-", "gte-", "granite-embedding"}
	lower := strings.ToLower(name)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
