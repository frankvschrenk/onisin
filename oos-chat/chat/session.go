package chat

// session.go — generic eino ReAct session driven by a Mode.
//
// The host supplies a Mode (tools + prompt + assistant-message hook)
// and a chat model name; the Session handles the eino plumbing:
// callback wiring for live tool-call notifications, history
// management, and trimming long tool results so the context window
// doesn't blow up across turns.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	ucb "github.com/cloudwego/eino/utils/callbacks"
)

// ToolEvent carries information about a tool call for the UI.
type ToolEvent struct {
	Name  string
	Input string // JSON args from the LLM, or tool result on OnEnd
}

// Session is a single ReAct conversation against one Mode.
type Session struct {
	agent     *react.Agent
	history   []*schema.Message
	modelName string
	mode      Mode
}

// NewSession constructs a Session bound to the given mode and model.
// modelName="" picks the default via llm.FirstChatModel.
func NewSession(ctx context.Context, mode Mode, modelName string) (*Session, error) {
	if mode == nil {
		return nil, fmt.Errorf("chat session: mode is required")
	}
	cm, resolvedName, err := newChatModel(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("chat session: %w", err)
	}

	sysPrompt := mode.SystemPrompt()
	tools := mode.Tools()
	log.Printf("[oos-chat] mode=%s model=%s tools=%d prompt=%dch",
		mode.Name(), resolvedName, len(tools), len(sysPrompt))

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: cm,
		MaxStep:          20,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: tools,
		},
		MessageRewriter: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			return trimToolResults(msgs, 800)
		},
		MessageModifier: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			if sysPrompt == "" {
				return msgs
			}
			sys := schema.SystemMessage(sysPrompt)
			return append([]*schema.Message{sys}, msgs...)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("chat session: agent: %w", err)
	}

	return &Session{
		agent:     agent,
		modelName: resolvedName,
		mode:      mode,
	}, nil
}

// ModelName returns the resolved model name.
func (s *Session) ModelName() string { return s.modelName }

// Mode returns the Mode bound to this session.
func (s *Session) Mode() Mode { return s.mode }

// MessageCount returns the number of messages in the conversation
// history (system messages are not counted; they are re-attached on
// every turn by the MessageModifier).
func (s *Session) MessageCount() int { return len(s.history) }

// Reset clears the conversation history. The agent and tools stay.
func (s *Session) Reset() { s.history = nil }

// Send submits a user message and returns the final assistant reply.
// onToolStart fires immediately when the agent starts a tool call;
// onToolEnd fires when the tool finishes. Both may be nil.
func (s *Session) Send(
	ctx context.Context,
	userMessage string,
	onToolStart func(ToolEvent),
	onToolEnd func(ToolEvent),
) (string, error) {
	s.history = append(s.history, &schema.Message{
		Role:    schema.User,
		Content: userMessage,
	})

	toolHandler := &ucb.ToolCallbackHandler{
		OnStart: func(_ context.Context, info *callbacks.RunInfo, in *tool.CallbackInput) context.Context {
			if onToolStart != nil {
				onToolStart(ToolEvent{Name: info.Name, Input: prettyJSON(in.ArgumentsInJSON)})
			}
			return ctx
		},
		OnEnd: func(_ context.Context, info *callbacks.RunInfo, out *tool.CallbackOutput) context.Context {
			if onToolEnd != nil {
				onToolEnd(ToolEvent{Name: info.Name, Input: out.Response})
			}
			return ctx
		},
		OnError: func(_ context.Context, info *callbacks.RunInfo, err error) context.Context {
			if onToolEnd != nil {
				onToolEnd(ToolEvent{Name: info.Name, Input: "error: " + err.Error()})
			}
			return ctx
		},
	}
	cb := react.BuildAgentCallback(nil, toolHandler)
	opt := agent.WithComposeOptions(compose.WithCallbacks(cb))

	resp, err := s.agent.Generate(ctx, s.history, opt)
	if err != nil {
		return "", fmt.Errorf("agent generate: %w", err)
	}
	s.history = append(s.history, resp)
	log.Printf("[oos-chat] mode=%s response: %d chars", s.mode.Name(), len(resp.Content))
	return resp.Content, nil
}

// trimToolResults shortens tool result messages over maxRunes runes
// so the context window stays bounded across multi-turn chats.
func trimToolResults(msgs []*schema.Message, maxRunes int) []*schema.Message {
	out := make([]*schema.Message, len(msgs))
	for i, m := range msgs {
		if m.Role == schema.Tool && len([]rune(m.Content)) > maxRunes {
			cp := *m
			cp.Content = string([]rune(m.Content)[:maxRunes]) + "...(truncated)"
			out[i] = &cp
		} else {
			out[i] = m
		}
	}
	return out
}

// prettyJSON pretty-prints a JSON string for log/UI display, falling
// back to the raw input if it is not valid JSON.
func prettyJSON(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(b)
}
