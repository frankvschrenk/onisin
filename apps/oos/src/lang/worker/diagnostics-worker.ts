// diagnostics-worker.ts — Web Worker that parses and validates onisin
// DSL source through the oos-dsls-ts services.
//
// Direct port of oosd's diagnostics-worker.ts so both apps share one
// worker contract. apps/oos only uses the parseView path today (and
// keeps the parseDomain entrypoint reserved for later). The validate
// path is unused but cheap to keep — it doesn't pull in extra deps.
//
// Three protocols share this worker:
//
//   "validate":    parse + validate, return LSP diagnostics
//                  (used by oosd's editor red-squiggle pipeline; not
//                  invoked by apps/oos today).
//
//   "parseView":   parse + map AST → ViewDef, return the runtime
//                  ViewDef plus diagnostics. The renderer in apps/oos
//                  uses this so Langium stays out of the main bundle —
//                  Electrobun's main-thread bundler mishandles
//                  vscode-jsonrpc's cancellation namespace import,
//                  same root cause that made us pick esbuild for the
//                  worker bundle in the first place.
//
//   "parseDomain": parse + map AST → DomainDef. Reserved entrypoint
//                  for a domain-aware path (e.g. richer column
//                  formatting from the bound domain).
//
// All replies share the request `id` for correlation; debounced
// callers compare with the latest dispatched id and drop stale
// replies.

import type { Diagnostic } from "vscode-languageserver-types";
import {
	createOnisinServices,
	mapDomain,
	mapView,
	type DomainDef,
	type OnisinLanguage,
	type ViewDef,
} from "oos-dsls-ts";

// One shared services instance per worker — both languages live on
// the same shared registry, so this single call covers .domain and
// .view alike.
const services = createOnisinServices();

// ─── Message protocol ────────────────────────────────────────────────

/** Request to validate a buffer (editor diagnostics path). */
type ValidateRequest = {
	type: "validate";
	id: number;
	language: OnisinLanguage;
	uri: string;
	text: string;
};

/** Request to parse a `.view` buffer and return its runtime ViewDef. */
type ParseViewRequest = {
	type: "parseView";
	id: number;
	uri: string;
	text: string;
};

/** Request to parse a `.domain` buffer and return its runtime DomainDef. */
type ParseDomainRequest = {
	type: "parseDomain";
	id: number;
	uri: string;
	text: string;
};

type WorkerRequest = ValidateRequest | ParseViewRequest | ParseDomainRequest;

/** Reply for a "validate" request. */
type DiagnosticsReply = {
	type: "diagnostics";
	id: number;
	uri: string;
	diagnostics: Diagnostic[];
};

/** Reply for a "parseView" request. */
type ViewDefReply = {
	type: "viewDef";
	id: number;
	uri: string;
	def: ViewDef | undefined;
	diagnostics: Diagnostic[];
};

/** Reply for a "parseDomain" request. */
type DomainDefReply = {
	type: "domainDef";
	id: number;
	uri: string;
	def: DomainDef | undefined;
	diagnostics: Diagnostic[];
};

// ─── Worker entry ────────────────────────────────────────────────────

self.onmessage = async (event: MessageEvent<WorkerRequest>) => {
	const req = event.data;
	try {
		if (req.type === "validate") {
			await handleValidate(req);
			return;
		}
		if (req.type === "parseView") {
			await handleParseView(req);
			return;
		}
		if (req.type === "parseDomain") {
			await handleParseDomain(req);
			return;
		}
	} catch (err) {
		postFailure(req, err);
	}
};

async function handleValidate(req: ValidateRequest): Promise<void> {
	const result = await services.parse({
		language: req.language,
		uri: req.uri,
		text: req.text,
	});
	const reply: DiagnosticsReply = {
		type: "diagnostics",
		id: req.id,
		uri: req.uri,
		diagnostics: result.diagnostics,
	};
	self.postMessage(reply);
}

async function handleParseView(req: ParseViewRequest): Promise<void> {
	const result = await services.parse({
		language: "view",
		uri: req.uri,
		text: req.text,
	});
	const def = result.root ? mapView(result.root) : undefined;
	const reply: ViewDefReply = {
		type: "viewDef",
		id: req.id,
		uri: req.uri,
		def,
		diagnostics: result.diagnostics,
	};
	self.postMessage(reply);
}

async function handleParseDomain(req: ParseDomainRequest): Promise<void> {
	const result = await services.parse({
		language: "domain",
		uri: req.uri,
		text: req.text,
	});
	const def = result.root ? mapDomain(result.root) : undefined;
	const reply: DomainDefReply = {
		type: "domainDef",
		id: req.id,
		uri: req.uri,
		def,
		diagnostics: result.diagnostics,
	};
	self.postMessage(reply);
}

/**
 * postFailure surfaces a worker error in the shape expected by the
 * waiting client — diagnostics-shaped for "validate" requests, a
 * synthetic diagnostic + missing def for the parse* requests.
 */
function postFailure(req: WorkerRequest, err: unknown): void {
	const message = err instanceof Error ? err.message : String(err);
	const synthetic: Diagnostic = {
		severity: 1,
		range: {
			start: { line: 0, character: 0 },
			end: { line: 0, character: 0 },
		},
		message: `worker failure: ${message}`,
		source: "onisin-worker",
	};
	if (req.type === "validate") {
		const reply: DiagnosticsReply = {
			type: "diagnostics",
			id: req.id,
			uri: req.uri,
			diagnostics: [synthetic],
		};
		self.postMessage(reply);
		return;
	}
	if (req.type === "parseView") {
		const reply: ViewDefReply = {
			type: "viewDef",
			id: req.id,
			uri: req.uri,
			def: undefined,
			diagnostics: [synthetic],
		};
		self.postMessage(reply);
		return;
	}
	if (req.type === "parseDomain") {
		const reply: DomainDefReply = {
			type: "domainDef",
			id: req.id,
			uri: req.uri,
			def: undefined,
			diagnostics: [synthetic],
		};
		self.postMessage(reply);
	}
}
