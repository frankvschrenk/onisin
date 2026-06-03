// hooks.ts — React glue for ViewState and view-level actions.
//
// Three pieces:
//
//   1. ViewStateContext / useViewState — carries the ViewState
//      through the component tree. The renderer sets this once at
//      the top of a view; all bound widgets pick it up.
//
//   2. useBoundValue / useBoundOptions — subscribe to a single bind
//      path (or an option list) and re-render only when *that* slot
//      changes. Implementation uses `useSyncExternalStore` so
//      React's concurrent mode behaves correctly.
//
//   3. ViewActionsContext / useViewActions — carries the host's
//      `onAction` and `onToolbar` callbacks down to leaf renderers
//      (TableRenderer, future button widgets). Parallel to
//      ViewStateContext rather than prop-drilled because actions
//      can fire from any depth and the prop-drill noise scales
//      badly with body complexity.
//
// All three contexts have a defined "no provider" path so a
// preview / editor that omits the providers still renders cleanly:
// useViewState throws (a renderer bug — there's no sensible
// default), useViewActions returns an empty object (actions are
// silently ignored).

import { createContext, useCallback, useContext, useSyncExternalStore } from "react";
import type { DomainDef, ToolbarItemDef, ViewActionDef } from "oos-dsls-ts/types";
import type { ViewState, OptionEntry } from "./state";

/**
 * Context that carries the ViewState. The renderer wraps the view
 * tree in `<ViewStateProvider value={state}>...`.
 */
export const ViewStateContext = createContext<ViewState | null>(null);

/**
 * useViewState returns the ambient ViewState. Throws if used outside
 * a `<ViewStateProvider>`, which would mean a renderer bug.
 */
export function useViewState(): ViewState {
	const state = useContext(ViewStateContext);
	if (!state) {
		throw new Error(
			"useViewState: no ViewStateContext in scope. Wrap your view in <ViewStateProvider>.",
		);
	}
	return state;
}

/**
 * useBoundValue subscribes to a single bind path and returns
 * a `[value, setValue]` tuple. Re-renders the calling component
 * whenever the underlying value changes.
 *
 * Note: today's implementation re-runs `getSnapshot` on every state
 * mutation regardless of which key changed; the snapshot returns the
 * single-key value, so React still bails out when that value is
 * unchanged. Per-key subscriptions can come later if profiling shows
 * it matters.
 */
export function useBoundValue(bindPath: string): [string, (value: string) => void] {
	const state = useViewState();

	const subscribe = useCallback(
		(listener: () => void) => state.subscribe(listener),
		[state],
	);
	const getSnapshot = useCallback(() => state.get(bindPath), [state, bindPath]);

	const value = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
	const setValue = useCallback(
		(next: string) => state.set(bindPath, next),
		[state, bindPath],
	);

	return [value, setValue];
}

/**
 * useBoundOptions subscribes to an option list keyed by name (e.g.
 * "countries", "cities").
 */
export function useBoundOptions(key: string): readonly OptionEntry[] {
	const state = useViewState();
	const subscribe = useCallback(
		(listener: () => void) => state.subscribe(listener),
		[state],
	);
	const getSnapshot = useCallback(() => state.getOptions(key), [state, key]);
	return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

// ─── Actions context ─────────────────────────────────────────────────

/** Shape of the actions a host wires to a view. */
export interface ViewActionsValue {
	onAction?:  (action: ViewActionDef, row: Record<string, unknown>) => void;
	onToolbar?: (item:   ToolbarItemDef) => void;
}

/**
 * Context that carries onAction / onToolbar handlers from the host
 * (e.g. apps/oos's DetailRenderer / ViewRenderer) down to the leaf
 * renderers that emit actions (TableRenderer.row click, future
 * button on_click, etc).
 *
 * Default value is an empty object — a preview render without
 * action wiring will silently no-op rather than throw.
 */
export const ViewActionsContext = createContext<ViewActionsValue>({});

/**
 * useViewActions returns the ambient action handlers. Always safe
 * to call: missing handlers are returned as undefined, the leaf
 * renderer decides whether to no-op or hide an interactive
 * affordance entirely.
 */
export function useViewActions(): ViewActionsValue {
	return useContext(ViewActionsContext);
}

// ─── Domain context ──────────────────────────────────────────────────

/**
 * Carries the primary DomainDef alongside the ViewState. Widgets
 * that render dropdowns (select/multiselect/combobox/radio over
 * an `options=...` field) read the domain's `optionsRef` from
 * this context to find the right option list — no name-guessing
 * heuristic, no plural-mangling.
 *
 * Default null means "no domain in scope". Widgets that need it
 * fall back to an empty option list rather than throw, so a
 * preview render without a domain still produces a usable shell.
 */
export const ViewDomainContext = createContext<DomainDef | null>(null);

/**
 * useFieldOptionsKey returns the option-store key for the given
 * field name on the ambient domain, or undefined when the field
 * has no `optionsRef` declared (or no domain is in scope).
 *
 * The key is exactly the meta name from the .domain file (e.g.
 * "roles", "employment_types"), which matches what the envelope
 * loader stored under after `extractMetaPayload` stripped the
 * GraphQL `meta_` prefix.
 */
export function useFieldOptionsKey(fieldName: string): string | undefined {
	const domain = useContext(ViewDomainContext);
	if (!domain) return undefined;
	const field = domain.fields.find((f) => f.name === fieldName);
	return field?.optionsRef;
}
