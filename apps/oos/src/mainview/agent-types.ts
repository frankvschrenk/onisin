// agent-types.ts \u2014 agent/turn wire types for the webview seam.
//
// Inlined from the retired bun/agent/{types,loop} modules. In the Tauri/Pfad-C
// model the agent loop runs headless (Rust) and the webview only needs the
// request/response shapes for the chat/ask/translate/event turns plus the
// streamed AgentEvent union. The publishing/loop logic does not come along.

/** Connection + behaviour settings the turn endpoints need. */
export interface AgentSettings {
	llmBaseUrl: string;
	llmApiKey:  string;
	llmModel:   string;
	natsUrl:    string;
}

/**
 * Per-model fine-tuning knobs. Persisted in Dexie (one row per model) and
 * forwarded with every turn so the headless agent can apply temperature,
 * timeouts, response size and RAG breadth without reaching into renderer
 * storage.
 */
export interface AgentTuning {
	/** Sampling temperature passed straight to the LLM. */
	temperature: number;
	/** Hard ceiling for one /v1/chat/completions call, in ms. */
	timeoutMs:   number;
	/** Upper bound on the response length in tokens. */
	maxTokens:   number;
	/** Number of schema chunks oos_schema_search returns by default. */
	topHits:     number;
}

/** Tuning defaults used when a model has no persisted row yet. */
export const DEFAULT_TUNING: AgentTuning = {
	temperature: 0.2,
	timeoutMs:   5 * 60 * 1000,
	maxTokens:   4096,
	topHits:     10,
};

/** Summed token usage over an LLM call or a whole turn. */
export interface Usage {
	promptTokens:     number;
	completionTokens: number;
	totalTokens:      number;
}

/**
 * One message in the OpenAI chat-completions transcript. Distinct from the
 * renderer's scrollback ChatMessage (mainview/types.ts): this is the wire
 * shape the turn endpoints accept as `history`, named LlmMessage here to
 * avoid the clash.
 */
export type LlmMessage =
	| { role: "system"; content: string }
	| { role: "user"; content: string }
	| { role: "assistant"; content: string | null; tool_calls?: ToolCall[] }
	| { role: "tool"; tool_call_id: string; content: string };

/** A single tool-call as emitted by the LLM. */
export interface ToolCall {
	id:   string;
	type: "function";
	function: { name: string; arguments: string };
}

/**
 * Streaming events emitted by the headless agent for one turn. In Pfad C the
 * agent publishes these on a per-turn NATS subject; the seam subscribes and
 * fans them into the chat scrollback and the tabs store via subscribeAgentEvent.
 */
export type AgentEvent =
	| { type: "tool_call_start"; turnId: string; callId: string; name: string; args: unknown }
	| { type: "tool_call_end"; turnId: string; callId: string; name: string; ok: boolean; summary: string }
	| {
			type: "tab_open";
			turnId: string;
			contextName: string;
			/** Rows from oos.cmd.data.query (the agent's oos_query result). */
			rows: Record<string, unknown>[];
			/** Optional view name to render the result through (OnisinView). */
			viewName?: string;
	  }
	| { type: "assistant_message"; turnId: string; text: string }
	| { type: "agent_error"; turnId: string; message: string }
	| { type: "cancelled"; turnId: string };

/** Status of a finished turn. */
export type TurnStatus = "success" | "error" | "cancelled" | "step_limit";

/** A single tool invocation captured in the turn telemetry. */
export interface TraceToolCall {
	callId:     string;
	name:       string;
	args:       unknown;
	startedAt:  string;
	finishedAt: string;
	durationMs: number;
	ok:         boolean;
	result:     unknown;
}

/** Telemetry collected over the lifetime of one turn (Activity tab). */
export interface TurnTrace {
	startedAt:     string;
	finishedAt:    string;
	durationMs:    number;
	usage:         Usage;
	toolCalls:     TraceToolCall[];
	status:        TurnStatus;
	errorMessage?: string;
}
