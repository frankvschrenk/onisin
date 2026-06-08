// agent/types.ts \u2014 Shared agent types, relocated webview-side.
//
// Why here and not in a Bun gateway: in the Tauri/Pfad-C model the old
// bun/agent/ tree is gone \u2014 the ReAct loop itself is being re-homed in
// Rust. The webview still needs the *type* surface the agent turns speak
// (AgentEvent for the chat scrollback, TurnTrace for the Activity tab,
// AgentSettings/AgentTuning/ChatMessage for the chatTurn request), so the
// pure types are kept here, free of any transport dependency.
//
// AgentEvent/TurnTrace are also the wire contract the future Rust agent
// publishes per turnId over NATS; keeping them in one webview module means
// the seam (rpc.ts) and the panels share a single definition.

/** Connection + behaviour settings the loop needs. */
export interface AgentSettings {
	llmBaseUrl: string;
	llmApiKey:  string;
	llmModel:   string;
	natsUrl:    string;
}

/**
 * Per-model fine-tuning knobs. Persisted in IndexedDB on the mainview side
 * (one row per model) and forwarded in every chatTurn so the agent can
 * apply temperature, timeouts, response size and RAG breadth.
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

/** One message in the OpenAI chat-completions transcript. */
export type ChatMessage =
	| { role: "system"; content: string }
	| { role: "user"; content: string }
	| {
			role: "assistant";
			content: string | null;
			tool_calls?: ToolCall[];
	  }
	| { role: "tool"; tool_call_id: string; content: string };

/** A single tool-call as emitted by the LLM. */
export interface ToolCall {
	id:   string;
	type: "function";
	function: {
		name:      string;
		arguments: string;
	};
}

/** OpenAI-flavour function tool descriptor. */
export interface ToolSchema {
	type: "function";
	function: {
		name:        string;
		description: string;
		parameters:  Record<string, unknown>;
	};
}

/**
 * Streaming events emitted by an agent turn. The webview receives them
 * (over NATS in the Tauri model) and dispatches into the chat scrollback
 * and the tabs store.
 */
export type AgentEvent =
	| { type: "tool_call_start"; turnId: string; callId: string; name: string; args: unknown }
	| {
			type:    "tool_call_end";
			turnId:  string;
			callId:  string;
			name:    string;
			ok:      boolean;
			summary: string;
	  }
	| {
			type:    "tab_open";
			turnId:  string;
			contextName: string;
			/** Rows from oos.cmd.data.query (the agent's oos_query result). */
			rows:    Record<string, unknown>[];
			viewName?: string;
	  }
	| { type: "assistant_message"; turnId: string; text: string }
	| { type: "agent_error";       turnId: string; message: string }
	| { type: "cancelled";         turnId: string };

// \u2500\u2500\u2500 Turn telemetry (relocated from the old bun/agent/loop.ts) \u2500\u2500\u2500

/** Token accounting summed across a turn's LLM calls. */
export interface Usage {
	promptTokens:     number;
	completionTokens: number;
	totalTokens:      number;
}

/** Status of a finished turn. */
export type TurnStatus = "success" | "error" | "cancelled" | "step_limit";

/** A single tool invocation captured for the persisted turn record. */
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
	startedAt:  string;
	finishedAt: string;
	durationMs: number;
	usage:      Usage;
	toolCalls:  TraceToolCall[];
	status:     TurnStatus;
	errorMessage?: string;
}
