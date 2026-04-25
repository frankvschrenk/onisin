package chat

// mode.go — pluggable chat-mode contract.
//
// A Mode is everything the generic chat window needs to behave like
// a domain-specific assistant: which tools the LLM can call, which
// system prompt frames the conversation, and what to do with the
// final assistant message. Different consumers register different
// modes:
//
//   oos  Board mode      — tools manipulate the live board, answer
//                          appears as an assistant bubble.
//   oos  Events mode     — RAG over event streams, answer + sources
//                          shown in a side pane.
//   ooso DSL mode        — LLM emits a <screen> XML, OnAssistant
//                          forwards it to the DSL editor instead of
//                          (or in addition to) showing it in chat.
//
// A Mode is constructed by the host application and passed into
// OpenWindow. The host is therefore in full control of which tools
// are available and what happens with the assistant's output.

import "github.com/cloudwego/eino/components/tool"

// Mode is the contract every host implements to plug into the chat
// window. Implementations are typically small structs that close
// over host-side state (the active editor, the live board, ...).
type Mode interface {
	// Name is shown in the window title and used for logging.
	Name() string

	// Tools returns the eino tools the LLM is allowed to call in
	// this mode. Each turn rebuilds the agent against this list, so
	// modes can return a slice that depends on dynamic state.
	Tools() []tool.BaseTool

	// SystemPrompt is prepended to every turn. Empty is allowed —
	// the model then sees only the user message and tool descriptions.
	SystemPrompt() string

	// OnAssistantMessage is invoked once the agent settles on a final
	// reply. The chat window already shows the message as an
	// assistant bubble; this hook lets the host react with side
	// effects (push the text into a code editor, render a board,
	// open another window, ...). Errors are surfaced back into the
	// chat as an error bubble.
	OnAssistantMessage(text string) error
}
