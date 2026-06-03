// diagnostics-client.ts — main-thread side of the diagnostics path.
//
// Multiplexes through the shared worker (see ./worker.ts). Owns a
// debounced `validateModel` entry point that ships a Monaco model's
// text to the worker and writes the reply back as Monaco markers.

import * as monaco from "monaco-editor";
import type { Diagnostic } from "vscode-languageserver-types";

import type { Kind } from "../../mainview/types";
import { addReplyHandler, newRequestId, sharedWorker } from "./worker";

const OWNER = "onisin-langium";
const DEBOUNCE_MS = 200;

// ─── Reply routing ───────────────────────────────────────────────────

/**
 * pendingByModel tracks the most recently dispatched request id for
 * each Monaco model. Replies whose id no longer matches are stale and
 * dropped — this is how we recover from out-of-order completions.
 */
const pendingByModel = new Map<string, number>();
let installed = false;

function installReplyHandler(): void {
	if (installed) return;
	installed = true;
	addReplyHandler((event: MessageEvent) => {
		const msg = event.data;
		if (msg?.type !== "diagnostics") return;

		const model = monaco.editor.getModel(monaco.Uri.parse(msg.uri));
		if (!model) return;

		// Drop stale replies: only the latest dispatched id wins.
		if (pendingByModel.get(msg.uri) !== msg.id) return;

		const markers = (msg.diagnostics as Diagnostic[]).map(toMarker);
		monaco.editor.setModelMarkers(model, OWNER, markers);
	});
}

// ─── Debounce table ──────────────────────────────────────────────────

const debounceTimers = new Map<string, ReturnType<typeof setTimeout>>();

/**
 * validateModel debounces source updates per model and ships the
 * latest text to the worker. The reply path writes markers back via
 * monaco.editor.setModelMarkers.
 */
export function validateModel(model: monaco.editor.ITextModel, kind: Kind) {
	installReplyHandler();
	const key = model.uri.toString();
	const existing = debounceTimers.get(key);
	if (existing) clearTimeout(existing);

	const timer = setTimeout(() => {
		debounceTimers.delete(key);
		dispatch(model, kind);
	}, DEBOUNCE_MS);
	debounceTimers.set(key, timer);
}

function dispatch(model: monaco.editor.ITextModel, kind: Kind) {
	const w = sharedWorker();
	const id = newRequestId();
	const uri = model.uri.toString();
	pendingByModel.set(uri, id);
	// Map the panel Kind to the Langium OnisinLanguage id.
	// "event-types" uses the "event-schema" parser.
	const language = (kind as string) === "event-types" ? "event-schema" : kind;
	w.postMessage({
		type: "validate",
		id,
		language,
		uri,
		text: model.getValue(),
	});
}

// ─── LSP → Monaco severity mapping ───────────────────────────────────
//
// LSP severity: 1=Error, 2=Warning, 3=Info, 4=Hint
// Monaco severity (MarkerSeverity): 8=Error, 4=Warning, 2=Info, 1=Hint

function mapSeverity(s: number | undefined): monaco.MarkerSeverity {
	switch (s) {
		case 1:
			return monaco.MarkerSeverity.Error;
		case 2:
			return monaco.MarkerSeverity.Warning;
		case 3:
			return monaco.MarkerSeverity.Info;
		case 4:
			return monaco.MarkerSeverity.Hint;
		default:
			return monaco.MarkerSeverity.Error;
	}
}

function toMarker(d: Diagnostic): monaco.editor.IMarkerData {
	return {
		severity: mapSeverity(d.severity),
		message: d.message,
		// Monaco lines/columns are 1-based; LSP is 0-based.
		startLineNumber: d.range.start.line + 1,
		startColumn: d.range.start.character + 1,
		endLineNumber: d.range.end.line + 1,
		endColumn: d.range.end.character + 1,
		source: d.source,
	};
}

// ─── Cleanup ─────────────────────────────────────────────────────────

/**
 * clearMarkers wipes any onisin diagnostics for a model. Useful when
 * switching away from a row so stale red squiggles don't linger on
 * the next selection.
 */
export function clearMarkers(model: monaco.editor.ITextModel) {
	monaco.editor.setModelMarkers(model, OWNER, []);
}
