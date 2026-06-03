// mapper/common.ts — Shared AST → runtime conversions used by both
// the view and domain mappers.
//
// All helpers here are pure: AST in, runtime def out, no I/O.
// Any required cross-references (e.g. resolving a domain field for a
// FieldRef) belong in higher-level mappers, not here.

import type {
	FieldRef,
	FormatToken,
	Gap,
	LayoutMod,
	Margin,
	Padding,
	ScrollFlag,
	SpacingValue,
} from "../generated/ast";
import type {
	FieldRefDef,
	FormatDef,
	LayoutModDef,
	SpacingDef,
} from "../types";

/** Convert a parsed `<domain>.<field>` reference to its runtime form. */
export function mapFieldRef(ref: FieldRef): FieldRefDef {
	return { domain: ref.domain, field: ref.field };
}

/**
 * Convert a `FormatToken` (e.g. `currency`, `number:0`) to FormatDef.
 *
 * The grammar's `FormatDetail` data-type rule returns string, so a
 * numeric digit count like `2` arrives here as the string "2" even
 * though the AST type union allows `number`. Coerce numeric-looking
 * strings to actual numbers so the renderer can pass them straight
 * to `Intl.NumberFormat`.
 */
export function mapFormat(token: FormatToken): FormatDef {
	let detail: FormatDef["detail"];
	const raw = token.detail;
	if (typeof raw === "number") {
		detail = raw;
	} else if (typeof raw === "string") {
		if (/^\d+$/.test(raw)) {
			detail = Number(raw);
		} else {
			detail = raw as FormatDef["detail"];
		}
	}
	return { kind: token.kind, detail };
}

/** Convert a SpacingValue (token name or numeric scale) to SpacingDef. */
export function mapSpacing(value: SpacingValue): SpacingDef {
	if (value.$type === "SpacingToken") {
		return { kind: "token", value: value.token };
	}
	return { kind: "scale", value: value.value };
}

/**
 * Map the heterogenous `LayoutMod` union to a uniform LayoutModDef.
 *
 * Returns `null` for any AST node we don't recognise; callers filter
 * those out so a future grammar addition doesn't crash existing code.
 */
export function mapLayoutMod(mod: LayoutMod): LayoutModDef | null {
	switch (mod.$type) {
		case "Padding": {
			const p = mod as Padding;
			return { kind: "padding", prop: p.prop, value: mapSpacing(p.value) };
		}
		case "Margin": {
			const m = mod as Margin;
			return { kind: "margin", prop: m.prop, value: mapSpacing(m.value) };
		}
		case "Gap": {
			const g = mod as Gap;
			return { kind: "gap", value: mapSpacing(g.value) };
		}
		case "Cols":
			return { kind: "cols", value: mod.value };
		case "Expand":
			return { kind: "expand" };
		case "ScrollFlag": {
			const s = mod as ScrollFlag;
			return { kind: "scroll", value: s.scroll === "true" };
		}
		default:
			return null;
	}
}

/** Helper: map an array of LayoutMod, dropping unknown nodes. */
export function mapLayoutMods(mods: LayoutMod[] | undefined): LayoutModDef[] {
	if (!mods) return [];
	const out: LayoutModDef[] = [];
	for (const m of mods) {
		const def = mapLayoutMod(m);
		if (def) out.push(def);
	}
	return out;
}
