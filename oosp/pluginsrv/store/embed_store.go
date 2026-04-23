package store

// embed_store.go — OpenAI-compatible embedding store.
//
// Speaks to any endpoint that implements POST /v1/embeddings:
//   - Ollama  (http://localhost:11434)
//   - vLLM    (configurable)
//   - Ray     (configurable)
//   - Any remote provider with an OpenAI-compatible API
//
// The embedding model must be available at the configured endpoint, e.g.:
//   ollama pull ibm/granite-embedding:107m-multilingual

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// EmbedStore generates embedding vectors for text.
type EmbedStore interface {
	// Embed returns the embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Model returns the embedding model name.
	Model() string
}

// OpenAIEmbedStore calls any OpenAI-compatible /v1/embeddings endpoint.
type OpenAIEmbedStore struct {
	baseURL string // always ends with /v1
	apiKey  string // empty for local models
	model   string
}

// NewOpenAIEmbedStore creates an EmbedStore for the given OpenAI-compatible endpoint.
// baseURL should not include /v1 — it is appended automatically unless already present.
func NewOpenAIEmbedStore(baseURL, apiKey, model string) *OpenAIEmbedStore {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "granite-embedding:latest"
	}
	u := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(u, "/v1") {
		u += "/v1"
	}
	return &OpenAIEmbedStore{baseURL: u, apiKey: apiKey, model: model}
}

// Model returns the configured embedding model name.
func (s *OpenAIEmbedStore) Model() string { return s.model }

// Embed sends text to the /v1/embeddings endpoint and returns the vector.
func (s *OpenAIEmbedStore) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{Model: s.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}

	var lastErr error
	for attempt := range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			s.baseURL+"/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("embed: request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if s.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+s.apiKey)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("embed: endpoint not reachable (%s): %w", s.baseURL, err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("embed: HTTP %d from %s", resp.StatusCode, s.baseURL)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("embed: HTTP %d from %s", resp.StatusCode, s.baseURL)
		}

		var result struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("embed: response parse: %w", err)
		}
		resp.Body.Close()

		if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
			return nil, fmt.Errorf("embed: empty response (model=%s, attempt=%d)", s.model, attempt+1)
		}
		return result.Data[0].Embedding, nil
	}
	return nil, lastErr
}

// NewEmbedStore creates an EmbedStore from configuration.
// Currently only the openai-compatible backend is supported.
func NewEmbedStore(baseURL, apiKey, model string) (EmbedStore, error) {
	return NewOpenAIEmbedStore(baseURL, apiKey, model), nil
}
