// store-types.ts \u2014 persisted row shapes, inlined webview-side.
//
// Relocated from the retired oos-store-ts package. In the old app the Bun
// gateway owned a bun:sqlite file and these were its row shapes; in the
// Tauri model the same rows live in the local Dexie database (see
// store/db.ts) and the seam (rpc.ts) reads/writes them directly. toolCalls /
// usage / chunks stay JSON strings so the on-disk shape is unchanged and the
// existing record<->row codecs in turns.ts / pipeline-steps.ts keep working.

/** One persisted turn-telemetry row. */
export interface TurnRow {
	id:           string;
	chatId:       string | null;
	createdAt:    string;
	finishedAt:   string;
	durationMs:   number;
	model:        string;
	llmBaseUrl:   string;
	userText:     string;
	viewHint:     string | null;
	finalText:    string;
	status:       string;
	steps:        number;
	/** JSON-encoded TurnToolCall[]. */
	toolCalls:    string;
	/** JSON-encoded TurnUsage. */
	usage:        string;
	errorMessage: string | null;
}

/** One persisted pipeline-step output row (companion to TurnRow). */
export interface PipelineStepOutputRow {
	turnId:     string;
	stepIndex:  number;
	stepName:   string;
	stepKind:   string;
	summary:    string;
	/** JSON-encoded string[]. */
	chunks:     string;
	durationMs: number;
	/** JSON-encoded StepUsage. */
	usage:      string;
	createdAt:  string;
}
