// envelope.ts — Load a backend response envelope into a ViewState
// and build outbound mutation payloads from a snapshot.
//
// Direct port of the Go helpers `dsl.State.LoadJSON()` and
// `dsl.BuildEvent()` from oos-dsl_old/dsl/state.go and json.go. The
// envelope shape is unchanged so the existing oosp endpoints work
// without modification:
//
//   {
//     "content": { "person": { "firstname": "Frank" } },
//     "meta":    { "countries": [{value, label}, ...] }
//   }
//
// Two transitional shapes are still accepted for compatibility with
// older oosp responses: `{ data, options }` (same semantics, older
// names) and a flat object (no envelope at all).

import type { OptionEntry, ViewState } from "./state";

/**
 * Inbound envelope is `unknown`-typed at the boundary because it
 * arrives from JSON parsing (REST or mock data). Internal walks
 * use `isObject` / `Array.isArray` to refine before reading.
 */
export type EnvelopeObject = { readonly [key: string]: unknown };

/**
 * loadEnvelope hydrates a ViewState from an oosp response envelope.
 *
 * Both `content` and `meta` are independent: opening a "new" entity
 * with empty content but populated meta still loads the dropdown
 * options, mirroring the Go behaviour added there for the same
 * reason.
 */
export function loadEnvelope(state: ViewState, raw: EnvelopeObject): void {
	const hasContent = "content" in raw;
	const hasMeta = "meta" in raw;
	if (hasContent || hasMeta) {
		const content = raw.content;
		if (isObject(content)) {
			flattenInto(state, "", content);
		}
		const meta = raw.meta;
		if (isObject(meta)) {
			loadOptions(state, meta);
		}
		return;
	}

	// Transitional: `data` + `options`, identical semantics.
	const hasData = "data" in raw;
	const hasOptions = "options" in raw;
	if (hasData || hasOptions) {
		const data = raw.data;
		if (isObject(data)) {
			flattenInto(state, "", data);
		}
		const options = raw.options;
		if (isObject(options)) {
			loadOptions(state, options);
		}
		return;
	}

	// Legacy: flat JSON. No options in this shape.
	flattenInto(state, "", raw);
}

/**
 * buildEvent produces the flat outbound JSON payload for /mutation.
 *
 * Behaves like the Go `BuildEvent`: the first dot-prefix is dropped,
 * so `{"person.id": "42", "person.firstname": "Frank"}` becomes
 * `{"id": "42", "firstname": "Frank"}` — the shape oosp expects.
 *
 * @param screenID  the view id (currently unused in the payload, kept
 *                  for parity with the Go signature in case oosp
 *                  starts requiring it later).
 * @param action    likewise reserved.
 * @param state     state to snapshot.
 */
export function buildEvent(
	screenID: string,
	action: string,
	state: ViewState,
): Record<string, string> {
	void screenID;
	void action;
	const snap = state.snapshot();
	const out: Record<string, string> = {};
	for (const [key, value] of Object.entries(snap)) {
		const idx = key.indexOf(".");
		const flatKey = idx >= 0 ? key.substring(idx + 1) : key;
		out[flatKey] = value;
	}
	return out;
}

// ─── Internals ───────────────────────────────────────────────────────

function isObject(v: unknown): v is EnvelopeObject {
	return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * flattenInto walks a nested object, joining keys with `.` and storing
 * the leaves as strings on the state. Arrays are stored verbatim as
 * JSON strings — that mirrors the Go behaviour so table bindings
 * (`person.rows`) work the same way.
 */
function flattenInto(state: ViewState, prefix: string, value: unknown): void {
	if (isObject(value)) {
		for (const [k, child] of Object.entries(value)) {
			const key = prefix === "" ? k : `${prefix}.${k}`;
			flattenInto(state, key, child);
		}
		return;
	}
	if (Array.isArray(value)) {
		state.set(prefix, JSON.stringify(value));
		return;
	}
	state.set(prefix, fmtValue(value));
}

/**
 * loadOptions fills the option store from the `meta` section.
 * Accepts entries shaped as `{value, label}` objects or raw strings
 * (where value === label).
 */
function loadOptions(state: ViewState, meta: EnvelopeObject): void {
	for (const [key, raw] of Object.entries(meta)) {
		if (!Array.isArray(raw)) continue;
		const entries: OptionEntry[] = [];
		for (const item of raw) {
			if (isObject(item)) {
				const value = fmtValue(item.value);
				const label = fmtValue(item.label) || value;
				entries.push({ value, label });
			} else if (typeof item === "string") {
				entries.push({ value: item, label: item });
			}
		}
		state.setOptions(key, entries);
	}
}

/** Coerce primitive scalars to string the way the Go fmtValue does. */
function fmtValue(v: unknown): string {
	if (v === undefined || v === null) return "";
	if (typeof v === "string") return v;
	if (typeof v === "number" || typeof v === "boolean") return String(v);
	return "";
}
