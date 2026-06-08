// permissions.ts — Role-scoped action gating against a DomainDef.
//
// Pure function: takes a domain, a role, and an action; returns
// whether the role has that action on the domain. No DB, no GraphQL,
// no I/O. Both oosgql (the future Hono server) and oosd (for editor-
// side preview gating) call this from completely different contexts.
//
// "Action" here is read / write / delete, the verbs the domain DSL
// declares. write covers both update and insert because the legacy
// system never distinguished — a role that may write is allowed to
// create, mirroring the way `oos_save` worked.

import type { DomainDef, PermissionActionDef } from "oos-dsls-ts";

/**
 * isActionAllowed returns true when the given role has the given
 * action on the given domain.
 *
 * Roles not declared in the domain are treated as having no
 * permissions — fail-closed. Same for unknown actions.
 */
export function isActionAllowed(
	domain: DomainDef,
	role: string,
	action: PermissionActionDef,
): boolean {
	for (const p of domain.permissions) {
		if (p.role !== role) continue;
		return p.actions.includes(action);
	}
	return false;
}

/**
 * assertActionAllowed is the throwing variant. The error message is
 * intentionally minimal — callers (the GraphQL layer or the future
 * REST endpoints) decide how to surface this; some need a 403 status,
 * others want a structured GraphQL error.
 */
export function assertActionAllowed(
	domain: DomainDef,
	role: string,
	action: PermissionActionDef,
): void {
	if (!isActionAllowed(domain, role, action)) {
		throw new Error(`role "${role}" not allowed to ${action} ${domain.name}`);
	}
}
