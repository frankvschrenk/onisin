// parse-client.ts — main-thread side of the parsing path.
//
// Ships .view and .domain source to the shared worker, which parses
// it and runs the AST → runtime mapper, then resolves a Promise with
// the result.
//
// Why a worker round-trip and not a direct `parseView` / `parseDomain`
// call: Langium pulls in vscode-jsonrpc whose cancellation namespace
// import trips up Electrobun's main-thread bundler ("Can't find
// variable: exports_cancellation"). The worker bundle is built by
// esbuild which handles that import shape correctly, so all Langium
// use stays inside the worker.
//
// Multiple in-flight parse requests are tolerated; each gets a
// unique id and the reply handler resolves the matching promise.
//
// Direct port of oosd's parse-client.ts. The two are kept byte-for-
// byte identical so the worker contract stays one thing — when the
// worker needs to grow a new request type, both clients update at
// the same time.

import type { Diagnostic } from "vscode-languageserver-types";
import type { DomainDef, ViewDef } from "oos-dsls-ts";

import { addReplyHandler, newRequestId, sharedWorker } from "./worker";

/** Result mirrors `parseView()` from oos-dsls-ts. */
export interface ParseViewResult {
	def: ViewDef | undefined;
	diagnostics: Diagnostic[];
}

/** Result mirrors `parseDomain()` from oos-dsls-ts. */
export interface ParseDomainResult {
	def: DomainDef | undefined;
	diagnostics: Diagnostic[];
}

type PendingView = {
	resolve: (r: ParseViewResult) => void;
	reject: (err: unknown) => void;
};

type PendingDomain = {
	resolve: (r: ParseDomainResult) => void;
	reject: (err: unknown) => void;
};

const pendingViews = new Map<number, PendingView>();
const pendingDomains = new Map<number, PendingDomain>();
let installed = false;

/**
 * installReplyHandler wires one onmessage handler that fans out the
 * worker's two reply types ("viewDef", "domainDef") to the matching
 * pending map. Idempotent — only the first call attaches.
 */
function installReplyHandler(): void {
	if (installed) return;
	installed = true;
	addReplyHandler((event: MessageEvent) => {
		const msg = event.data;
		if (msg?.type === "viewDef") {
			const p = pendingViews.get(msg.id);
			if (!p) return;
			pendingViews.delete(msg.id);
			p.resolve({ def: msg.def, diagnostics: msg.diagnostics });
			return;
		}
		if (msg?.type === "domainDef") {
			const p = pendingDomains.get(msg.id);
			if (!p) return;
			pendingDomains.delete(msg.id);
			p.resolve({ def: msg.def, diagnostics: msg.diagnostics });
		}
	});
}

/**
 * parseViewInWorker ships `text` to the worker and resolves with
 * the resulting ViewDef plus diagnostics.
 *
 * @param text - the .view source.
 * @param uri  - opaque identifier used by the worker for diagnostics
 *               URI; safe to reuse across parses (e.g. the view id).
 */
export function parseViewInWorker(text: string, uri: string): Promise<ParseViewResult> {
	installReplyHandler();
	const w = sharedWorker();
	const id = newRequestId();
	return new Promise<ParseViewResult>((resolve, reject) => {
		pendingViews.set(id, { resolve, reject });
		w.postMessage({ type: "parseView", id, uri, text });
	});
}

/**
 * parseDomainInWorker ships `.domain` source to the worker and
 * resolves with the resulting DomainDef plus diagnostics. Used by
 * the auto-mock generator: the preview needs the DomainDef to
 * fabricate placeholder data shaped like the real schema.
 *
 * Not used in apps/oos today — kept for parity with the oosd
 * pipeline so adding a domain-aware path later is a one-import
 * change rather than a worker contract change.
 */
export function parseDomainInWorker(
	text: string,
	uri: string,
): Promise<ParseDomainResult> {
	installReplyHandler();
	const w = sharedWorker();
	const id = newRequestId();
	return new Promise<ParseDomainResult>((resolve, reject) => {
		pendingDomains.set(id, { resolve, reject });
		w.postMessage({ type: "parseDomain", id, uri, text });
	});
}
