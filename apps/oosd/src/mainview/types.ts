// types.ts — shared types for the oosd mainview.
//
// The Kind discriminator picks which panel the main area shows.

export type Kind =
	| "domain"
	| "view"
	| "events"
	| "event-types"
	| "mapping-types"
	| "grammar"
	| "kv-store"
	| "iam"
	| "settings"
	| "demo";

/** Connection lifecycle states surfaced by the connect bar. */
export type Status =
	| { kind: "idle" }
	| { kind: "connecting" }
	| { kind: "connected" }
	| { kind: "error"; message: string };
