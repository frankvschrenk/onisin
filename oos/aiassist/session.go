package aiassist

// session.go — eino ReAct agent session with live callback notifications.
//
// Uses react.BuildAgentCallback with ToolCallbackHandler.OnStart so tool calls
// appear in the UI immediately when the agent decides to call them, not after
// they complete.

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

	"onisin.com/oos/helper"
	ootools "onisin.com/oos/tools"
)

// Session holds the state of a single conversation with the OOS AI assistant.
type Session struct {
	agent        *react.Agent
	history      []*schema.Message
	modelName    string
	schemaPrompt string // loaded once at session start
}

// NewSession creates a new eino ReAct agent session using the default
// chat model resolved by newChatModel.
func NewSession(ctx context.Context) (*Session, error) {
	return NewSessionWithModel(ctx, "")
}

// NewSessionWithModel creates a new session bound to a specific chat model.
// Passing "" falls back to the default (NewSession behaviour).
func NewSessionWithModel(ctx context.Context, modelName string) (*Session, error) {
	cm, resolvedName, err := newChatModelWithName(ctx, modelName)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}

	// schemaPrompt is loaded before the agent is created so the MessageModifier
	// can embed it as a closure without needing a setter on the agent.
	schemaPrompt := ootools.BuildSchemaPrompt()
	log.Printf("[aiassist] schema strategy: %s (%d chars)",
		helper.LLMSchemaStrategy, len(schemaPrompt))

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: cm,
		MaxStep: 20,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: buildTools(),
		},
		// ToolReturnDirectly: after these tools the agent returns immediately
		// without a second LLM call for a summary. The board opens and that
		// is answer enough.
		ToolReturnDirectly: map[string]struct{}{
			"oos_query":  {},
			"oos_render": {},
		},
		// MessageRewriter trims tool results in the history so they don't
		// inflate the context window across multiple turns.
		MessageRewriter: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			return trimToolResults(msgs, 400)
		},
		MessageModifier: func(_ context.Context, msgs []*schema.Message) []*schema.Message {
			sys := schema.SystemMessage(buildSystemPrompt(schemaPrompt))
			return append([]*schema.Message{sys}, msgs...)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("session: agent: %w", err)
	}

	return &Session{
		agent:        agent,
		modelName:    resolvedName,
		schemaPrompt: schemaPrompt,
	}, nil
}

// ModelName returns the model name used by this session.
func (s *Session) ModelName() string { return s.modelName }

// ToolEvent carries information about a tool call for the UI.
type ToolEvent struct {
	Name  string
	Input string // JSON args from the LLM
}

// Send submits a user message and returns the final assistant response.
//
// onToolStart is called immediately when the agent starts a tool call —
// this fires before the tool executes so the UI updates in real time.
// onToolEnd is called when the tool finishes with its result (can be nil).
func (s *Session) Send(
	ctx context.Context,
	userMessage string,
	onToolStart func(ev ToolEvent),
	onToolEnd func(ev ToolEvent),
) (string, error) {
	s.history = append(s.history, &schema.Message{
		Role:    schema.User,
		Content: userMessage,
	})

	// Build callback that fires immediately when a tool is about to run.
	toolHandler := &ucb.ToolCallbackHandler{
		OnStart: func(_ context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
			if onToolStart != nil {
				args := prettyJSON(input.ArgumentsInJSON)
				onToolStart(ToolEvent{Name: info.Name, Input: args})
			}
			return ctx
		},
		OnEnd: func(_ context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
			if onToolEnd != nil {
				onToolEnd(ToolEvent{Name: info.Name, Input: output.Response})
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
	log.Printf("[aiassist] response: %d chars", len(resp.Content))
	return resp.Content, nil
}

// Reset clears the conversation history. The agent and tools remain intact.
func (s *Session) Reset() {
	s.history = nil
}

// MessageCount returns the number of messages in the history.
func (s *Session) MessageCount() int {
	return len(s.history)
}

// trimToolResults shortens tool result messages in the history that exceed
// maxRunes characters. This keeps the context window small across multiple
// conversation turns without losing the conversation structure.
func trimToolResults(msgs []*schema.Message, maxRunes int) []*schema.Message {
	result := make([]*schema.Message, len(msgs))
	for i, m := range msgs {
		if m.Role == schema.Tool && len([]rune(m.Content)) > maxRunes {
			copy := *m
			copy.Content = string([]rune(m.Content)[:maxRunes]) + "...(truncated)"
			result[i] = &copy
		} else {
			result[i] = m
		}
	}
	return result
}

// prettyJSON formats a JSON string for display, falling back to the raw string.
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
