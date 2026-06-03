// mapper/view.ts — Top-level AST → ViewDef mapper.
//
// The single public entry point is `mapView()`. It receives the
// parsed `ViewModel` (the root AST node of a `.view` file) and
// returns the runtime ViewDef consumed by the renderer.

import type { ViewModel } from "../generated/ast";
import type { ToolbarItemDef, ViewDef, ViewDomainBindingDef } from "../types";
import { mapBodyElements, mapToolbarItem } from "./view-elements";

/**
 * Map a parsed ViewModel to a runtime ViewDef.
 *
 * The mapper is total: every input produces a ViewDef. Unknown body
 * elements are silently dropped (see `mapBodyElement`); empty or
 * missing arrays become empty arrays.
 *
 * Domain bindings come from `over <name>[(<alias>)] (, ...)`. The
 * mapper canonicalises them into `ViewDomainBindingDef` records:
 * a missing alias falls back to the domain name (so the legacy
 * single-domain shape `over person { … }` keeps working without
 * any change to consumers), and the first binding is flagged
 * primary.
 */
export function mapView(model: ViewModel): ViewDef {
	const v = model.view;
	const toolbar: ToolbarItemDef[] = [];
	for (const item of v.toolbar) {
		const def = mapToolbarItem(item);
		if (def) toolbar.push(def);
	}

	const domains: ViewDomainBindingDef[] = (v.domains ?? []).map((b, idx) => ({
		name:    b.name,
		alias:   b.alias ?? b.name,
		primary: idx === 0,
	}));

	const autoRefresh = v.autoRefresh
		? { domain: v.autoRefresh.domain, event: v.autoRefresh.event }
		: undefined;

	return {
		name:    v.name,
		title:   v.title,
		domains,
		default: v.default === true,
		autoRefresh,
		toolbar,
		body:    mapBodyElements(v.body),
	};
}
