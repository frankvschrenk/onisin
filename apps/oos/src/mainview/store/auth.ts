// store/auth.ts — Renderer-side auth state.
//
// Decodes the JWT payload from the token that bun sends via the
// authCompleted push message. Signature verification happens on
// the bun side; the renderer only reads display claims (username,
// role) from the already-trusted token.
//
// State is held in a plain module variable and exposed via a
// React hook so components re-render when it changes. Cleared
// on logout so no stale claims survive a user switch.

import { useSyncExternalStore } from "react";

// ─── Shape ────────────────────────────────────────────────────────────

export interface AuthClaims {
	username: string;
	role:     string;
	email:    string;
}

// ─── Module-level state ───────────────────────────────────────────────

let _state: AuthClaims | null = null;
const _listeners = new Set<() => void>();

function notify(): void {
	for (const fn of _listeners) fn();
}

function subscribe(fn: () => void): () => void {
	_listeners.add(fn);
	return () => { _listeners.delete(fn); };
}

function getSnapshot(): AuthClaims | null {
	return _state;
}

// ─── Public API ───────────────────────────────────────────────────────

/**
 * setClaims decodes a raw JWT access token and stores the display
 * claims. Called once `authCompleted` arrives with the token.
 * Does not verify the signature — bun already did that.
 */
export function setClaims(accessToken: string): void {
	try {
		const parts = accessToken.split(".");
		if (parts.length !== 3) { _state = null; notify(); return; }
		const padded   = (parts[1] ?? "").replace(/-/g, "+").replace(/_/g, "/");
		const payload  = JSON.parse(atob(padded)) as Record<string, unknown>;

		const email    = typeof payload["email"]              === "string" ? payload["email"]              : "";
		const prefName = typeof payload["preferred_username"] === "string" ? payload["preferred_username"] : "";
		const username = prefName || (email.includes("@") ? email.split("@")[0]! : email);

		// Resolve OOS role from groups (admin > manager > user).
		const rawGroups = payload["groups"];
		const groups: string[] = Array.isArray(rawGroups)
			? rawGroups.filter((g): g is string => typeof g === "string")
			: [];
		const PRIORITY: Record<string, number> = { admin: 3, manager: 2, user: 1 };
		let role = "";
		let best = 0;
		for (const g of groups) {
			for (const part of g.split("-")) {
				const p = PRIORITY[part] ?? 0;
				if (p > best) { best = p; role = part; }
			}
		}

		_state = { username, role, email };
	} catch {
		_state = null;
	}
	notify();
}

/**
 * clearClaims is called on logout. Resets to null so no stale
 * claims survive a user switch.
 */
export function clearClaims(): void {
	_state = null;
	notify();
}

/**
 * useAuthState is a React hook that returns the current auth
 * claims and re-renders when they change.
 */
export function useAuthState(): AuthClaims | null {
	return useSyncExternalStore(subscribe, getSnapshot);
}
