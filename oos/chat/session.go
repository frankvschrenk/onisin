package chat

import (
	"context"
	"fmt"
)

type Session struct {
	client   LLMClient
	messages []Message
	tools    []Tool
	useTools bool
}

func NewSession(client LLMClient, systemPrompt string, useTools bool) *Session {
	s := &Session{
		client:   client,
		useTools: useTools,
	}

	if systemPrompt != "" {
		s.messages = append(s.messages, Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	if useTools {
		s.tools = OOSTools()
	}

	return s
}

func (s *Session) Send(
	ctx context.Context,
	userMessage string,
	onChunk ChunkHandler,
	onToolCall func(toolName string),
) (string, error) {
	s.messages = append(s.messages, Message{
		Role:    "user",
		Content: userMessage,
	})

	const maxRounds = 5

	for round := 0; round < maxRounds; round++ {
		tools := s.tools
		if !s.useTools {
			tools = nil
		}

		text, toolCalls, err := s.client.Chat(ctx, s.messages, tools, onChunk)
		if err != nil {
			return "", fmt.Errorf("session: %w", err)
		}

		if len(toolCalls) == 0 {
			if text != "" {
				s.messages = append(s.messages, Message{
					Role:    "assistant",
					Content: text,
				})
			}
			return text, nil
		}

		s.messages = append(s.messages, Message{
			Role:    "assistant",
			Content: text,
		})

		for _, call := range toolCalls {
			if onToolCall != nil {
				onToolCall(call.Function.Name)
			}
			result := ExecuteToolCall(call)
			s.messages = append(s.messages, Message{
				Role:    "tool",
				Content: result,
			})
		}
	}

	return "", fmt.Errorf("session: maximale Tool-Call Runden erreicht")
}

func (s *Session) Reset() {
	if len(s.messages) > 0 && s.messages[0].Role == "system" {
		s.messages = s.messages[:1]
	} else {
		s.messages = nil
	}
}

func (s *Session) MessageCount() int {
	count := len(s.messages)
	if count > 0 && s.messages[0].Role == "system" {
		count--
	}
	return count
}
