// validation/view.ts — Custom validators for the view DSL.
//
// Today's checks focus on the `over` clause: a view may declare
// multiple participating domains, each with an optional alias.
// Aliases must be unique within the view (otherwise a bind path
// like `p.firstname` would be ambiguous), and the empty list is
// rejected as a parser-level safety net even though the grammar
// already requires at least one entry.
//
// Cross-document checks (does the named domain actually exist?)
// belong to a workspace-aware validator that walks oos.domain
// rows; they are tracked in the open-questions list and not
// implemented here.
//
// Adding more checks: write a method on ViewValidator and add
// an entry to the ValidationChecks map in viewChecks() below.

import type { ValidationAcceptor, ValidationChecks } from "langium";

import type { OnisinAstType, ViewDecl } from "../generated/ast.js";

/** Container for all view-level validation checks. */
export class ViewValidator {
	/**
	 * Reject duplicate aliases inside a single `over` clause. The
	 * diagnostic points at each offending binding's index so the
	 * editor highlights the second `(p)`, not the first one.
	 *
	 * The grammar guarantees at least one binding, but we still
	 * tolerate an empty list defensively — a future grammar
	 * change should not make this validator throw.
	 */
	checkUniqueAliases(decl: ViewDecl, accept: ValidationAcceptor): void {
		const seen = new Map<string, number>();
		for (let i = 0; i < decl.domains.length; i++) {
			const binding = decl.domains[i];
			if (!binding) continue;
			const alias = binding.alias ?? binding.name;
			const prev  = seen.get(alias);
			if (prev !== undefined) {
				accept(
					"error",
					`Alias '${alias}' is already used by domain ` +
						`'${decl.domains[prev]?.name ?? "?"}' in this view.`,
					{ node: binding, property: "alias" },
				);
				continue;
			}
			seen.set(alias, i);
		}
	}
}

/**
 * Build the ValidationChecks map for the view language. Wired up
 * by the view module override in services.ts.
 */
export function viewChecks(
	validator: ViewValidator,
): ValidationChecks<OnisinAstType> {
	return {
		ViewDecl: validator.checkUniqueAliases.bind(validator),
	};
}
