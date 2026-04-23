package aiassist

// event_session.go — RAG pipeline for the event chat mode.
//
// Flow per question:
//
//   1. POST /event/search  →  top-N EventHits (embed + pgvector happens server-side)
//   2. Build a context string from the hits
//   3. Call the LLM (eino ChatModel) with a system prompt + context + question
//   4. Return the LLM answer plus the raw hits so the UI can show both
//
// No tools, no agent loop — the search is deterministic and the LLM is used
// purely to synthesise an answer from the retrieved events.

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// EventQuery is the input to an event-mode chat turn.
type EventQuery struct {
	Mapping     string  // which event mapping to search (e.g. "police")
	StreamID    string  // optional; empty means search across all streams
	Question    string  // the user's natural-language question
	Limit       int     // max number of hits, 0 → server default
	MaxTokens   int     // response-length cap, 0 → library default
	Temperature float32 // sampling temperature, <0 → library default
}

// EventAnswer is the result of a single RAG turn.
type EventAnswer struct {
	Text  string     // LLM-synthesised answer in Markdown
	Hits  []EventHit // raw vector hits that fed the LLM
	Model string     // name of the LLM used
}

// eventSession wires a chat model to the oosp event endpoints.
//
// Reset() is a no-op because the RAG pipeline is stateless — every Ask()
// starts from scratch with only the current question. This keeps the
// behaviour predictable and avoids context-window drift.
type eventSession struct {
	model     model.ToolCallingChatModel
	modelName string
}

// newEventSession constructs a session using the default LLM configuration.
func newEventSession(ctx context.Context) (*eventSession, error) {
	return newEventSessionWithModel(ctx, "")
}

// newEventSessionWithModel constructs a session bound to a specific model name.
// Passing "" falls back to the default.
func newEventSessionWithModel(ctx context.Context, modelName string) (*eventSession, error) {
	cm, name, err := newChatModelWithName(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("event session: %w", err)
	}
	return &eventSession{model: cm, modelName: name}, nil
}

// ModelName returns the name of the chat model used by this session.
func (s *eventSession) ModelName() string { return s.modelName }

// Ask executes a full RAG turn and returns the answer plus sources.
//
// When the search returns no hits the LLM is not called at all — we
// short-circuit with a canned answer so the user isn't billed for a
// useless round-trip.
func (s *eventSession) Ask(ctx context.Context, q EventQuery) (*EventAnswer, error) {
	if strings.TrimSpace(q.Question) == "" {
		return nil, fmt.Errorf("question is empty")
	}
	if q.Mapping == "" {
		return nil, fmt.Errorf("mapping is empty")
	}

	// 1. Vector search on oosp.
	hits, err := SearchEvents(EventSearchRequest{
		Mapping:  q.Mapping,
		Query:    q.Question,
		StreamID: q.StreamID,
		Limit:    q.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("event search: %w", err)
	}
	if len(hits) == 0 {
		return &EventAnswer{
			Text:  "_No relevant events found for this question._",
			Hits:  nil,
			Model: s.modelName,
		}, nil
	}

	// 2. Build context + prompt.
	ctxText := buildEventContext(hits)
	sysMsg := schema.SystemMessage(eventSystemPrompt())
	userMsg := schema.UserMessage(fmt.Sprintf(
		"--- SOURCES ---\n%s\n--- END ---\n\nQuestion: %s",
		ctxText, q.Question,
	))

	// 3. Call the LLM (no tools).
	// WithMaxTokens overrides the backend's default response-length cap.
	// Ollama's num_predict default is 128 which truncates RAG answers
	// mid-sentence; raising it gives a synthesising LLM enough room.
	opts := []model.Option{}
	if q.MaxTokens > 0 {
		opts = append(opts, model.WithMaxTokens(q.MaxTokens))
	} else {
		opts = append(opts, model.WithMaxTokens(4096)) // sensible fallback
	}
	if q.Temperature >= 0 {
		opts = append(opts, model.WithTemperature(q.Temperature))
	}
	resp, err := s.model.Generate(ctx, []*schema.Message{sysMsg, userMsg}, opts...)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	return &EventAnswer{
		Text:  resp.Content,
		Hits:  hits,
		Model: s.modelName,
	}, nil
}

// buildEventContext turns a list of hits into the block we feed the LLM.
// Each hit becomes one labelled section so the LLM can reference sources
// by number in its answer.
//
// Both text_content and metadata are always included — metadata often holds
// structured facts (names, numbers, IDs) that the narrative text alone
// doesn't spell out, and those are exactly what the LLM needs to compose
// a complete answer.
func buildEventContext(hits []EventHit) string {
	var sb strings.Builder
	for i, h := range hits {
		sb.WriteString(fmt.Sprintf("[Source %d | type: %s | stream: %s | score: %.2f]\n",
			i+1, h.EventType, h.StreamID, h.Score))
		if h.TextContent != "" {
			sb.WriteString(h.TextContent)
			sb.WriteString("\n")
		}
		for k, v := range h.Metadata {
			sb.WriteString(fmt.Sprintf("%s: %v\n", k, v))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// eventSystemPrompt returns the system prompt used for all event-mode turns.
func eventSystemPrompt() string {
	return `You are an analysis assistant working with a set of event records.

Write a thorough, self-contained answer based on the sources.
Include concrete facts from the sources: names, dates, times, amounts,
locations, identifiers, descriptions. Do not omit relevant details just
to be concise — if the sources mention it and it relates to the question,
mention it.

Structure multi-part answers with short paragraphs or a bulleted list.
Reference sources inline with their [Source N] label.

Do not invent information. If the sources do not contain enough detail to
answer fully, say what is known and what is missing.

Answer in the same language as the question.`
}
