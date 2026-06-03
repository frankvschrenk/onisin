// chat-types.ts — chat message and settings shapes for the inline chat panel.
//
// Inlined from the retired bun/chat-rpc.ts. The ReAct chat agent has not
// yet moved into the webview, but the panel already renders history and a
// settings form against these shapes, so they live here independently of
// the agent port.

/** One turn in the chat transcript. */
export interface ChatMessage {
	role: "user" | "assistant" | "system";
	content: string;
}

/** LLM endpoint settings the chat panel sends with each turn. */
export interface ChatSettings {
	llmBaseUrl: string;
	llmApiKey: string;
	llmModel: string;
}
