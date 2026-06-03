// state.ts — Live values of all bound fields plus runtime options.
//
// This is the TypeScript counterpart to the Go `dsl.State` type from
// the legacy oos-dsl package. Identical concept: dot-separated bind
// paths (`person.firstname`, `person.address.city`) keyed against
// flat string values, plus per-key option lists for select/radio/
// combobox widgets.
//
// The store is observable: `subscribe()` returns an unsubscribe
// function and is called on every mutation. The bundled
// `useViewState()` hook in `hooks.ts` uses this for React-friendly
// re-renders via `useSyncExternalStore`.

/** A single entry in an options list (e.g. dropdown item). */
export interface OptionEntry {
	value: string;
	label: string;
}

/** Listener invoked after every mutating operation. */
export type StateListener = () => void;

/**
 * Shared sentinel returned for any missing options key. Reusing one
 * frozen array keeps the reference identity stable so React's
 * `useSyncExternalStore` does not see a "changed" snapshot on every
 * render — without this, missing-key reads cause an infinite loop.
 */
const EMPTY_OPTIONS: readonly OptionEntry[] = Object.freeze([]);

/**
 * State stores live form values and runtime option lists.
 *
 * Field values are stored as flat strings keyed by dot-path:
 *
 *   person.firstname           = "Frank"
 *   person.address.city        = "München"
 *
 * Option lists are stored separately keyed by an opaque name:
 *
 *   countries                  = [{value:"de", label:"Deutschland"}, ...]
 *
 * Both stores are mutated in place; subscribers re-read on demand.
 */
export class ViewState {
	private values = new Map<string, string>();
	private options = new Map<string, readonly OptionEntry[]>();
	private listeners = new Set<StateListener>();

	/** Get the current value for a bind path; "" when unset. */
	get(bindPath: string): string {
		return this.values.get(bindPath) ?? "";
	}

	/** Set a value and notify listeners. */
	set(bindPath: string, value: string): void {
		if (this.values.get(bindPath) === value) return;
		this.values.set(bindPath, value);
		this.notify();
	}

	/**
	 * Get the options list registered under `key`, or a stable empty
	 * array. The "stable" is important: `useSyncExternalStore` compares
	 * snapshots with Object.is, so returning a fresh `[]` on every miss
	 * would trigger an infinite render loop in React. The shared
	 * EMPTY_OPTIONS sentinel keeps the reference identity stable
	 * between calls.
	 */
	getOptions(key: string): readonly OptionEntry[] {
		return this.options.get(key) ?? EMPTY_OPTIONS;
	}

	/** Register an options list. Replaces any prior entry. */
	setOptions(key: string, entries: readonly OptionEntry[]): void {
		this.options.set(key, entries);
		this.notify();
	}

	/** Snapshot of all current values, copy-on-read. */
	snapshot(): Record<string, string> {
		const out: Record<string, string> = {};
		for (const [k, v] of this.values) out[k] = v;
		return out;
	}

	/**
	 * Subscribe to mutations. The returned function unsubscribes.
	 * Required signature for `useSyncExternalStore`.
	 */
	subscribe(listener: StateListener): () => void {
		this.listeners.add(listener);
		return () => {
			this.listeners.delete(listener);
		};
	}

	private notify(): void {
		for (const l of this.listeners) l();
	}
}
