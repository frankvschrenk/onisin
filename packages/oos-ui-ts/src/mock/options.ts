// options.ts — Generate dropdown option lists from a MetaDef.
//
// In the LLM-side contract every Meta produces a list of
// `{value, label}` pairs (see `oos-dsls-ts/renderer/llm-chunk`).
// The auto-mock generator follows the same shape: each declared
// Meta gets four hand-rolled entries whose flavour matches the
// meta's name when we recognise it.
//
// The generator is deliberately small. A real backend run reads
// from the actual lookup tables; the preview just needs *some*
// plausible options so dropdowns render with content.
//
// Every option list ends up under `meta.<metaName>` in the
// envelope's `meta` section, exactly where `loadEnvelope` looks
// for them.

import type { MetaDef } from "oos-dsls-ts";

/** One dropdown option as the renderer expects it. */
export interface MockOption {
	value: string;
	label: string;
}

/** Curated option presets keyed by Meta name (case-insensitive). */
const PRESETS: Readonly<Record<string, readonly MockOption[]>> = {
	cities: [
		{ value: "1", label: "München" },
		{ value: "2", label: "Berlin" },
		{ value: "3", label: "Hamburg" },
		{ value: "4", label: "Frankfurt" },
	],
	countries: [
		{ value: "de", label: "Deutschland" },
		{ value: "at", label: "Österreich" },
		{ value: "ch", label: "Schweiz" },
		{ value: "fr", label: "Frankreich" },
	],
	roles: [
		{ value: "admin", label: "Administrator" },
		{ value: "manager", label: "Manager" },
		{ value: "user", label: "Benutzer" },
		{ value: "guest", label: "Gast" },
	],
	departments: [
		{ value: "architecture", label: "Architektur" },
		{ value: "development", label: "Entwicklung" },
		{ value: "operations", label: "Betrieb" },
		{ value: "sales", label: "Vertrieb" },
	],
	statuses: [
		{ value: "draft", label: "Entwurf" },
		{ value: "active", label: "Aktiv" },
		{ value: "archived", label: "Archiviert" },
		{ value: "pending", label: "Wartend" },
	],
	priorities: [
		{ value: "low", label: "Niedrig" },
		{ value: "normal", label: "Normal" },
		{ value: "high", label: "Hoch" },
		{ value: "urgent", label: "Dringend" },
	],
};

/**
 * mockOptionsFor returns four option entries for a Meta. If the
 * meta's name matches a preset (case-insensitive, simple substring)
 * the curated list wins; otherwise we fabricate four entries from
 * the meta's name itself.
 *
 * Generated entries use sequential integer values (`1`..`4`) and
 * the meta name as the label root, so a meta `tags` becomes:
 *
 *   { value: "1", label: "tag 1" }
 *   { value: "2", label: "tag 2" }
 *   ...
 *
 * That keeps the preview readable and the value/label split
 * obvious to anyone inspecting the envelope.
 */
export function mockOptionsFor(meta: MetaDef): MockOption[] {
	const preset = matchPreset(meta.name);
	if (preset) return preset.slice();
	return generic(meta.name);
}

/**
 * matchPreset finds the first preset whose key appears as a
 * substring of the meta name. The order in PRESETS doesn't matter
 * — meta names are usually simple plurals, no overlap in practice.
 */
function matchPreset(name: string): readonly MockOption[] | undefined {
	const lower = name.toLowerCase();
	for (const [key, value] of Object.entries(PRESETS)) {
		if (lower.includes(key)) return value;
	}
	return undefined;
}

/** Generate four options from the meta's name as a fallback. */
function generic(name: string): MockOption[] {
	const root = name.toLowerCase().replace(/s$/, "") || "item";
	const out: MockOption[] = [];
	for (let i = 1; i <= 4; i++) {
		out.push({ value: String(i), label: `${root} ${i}` });
	}
	return out;
}
