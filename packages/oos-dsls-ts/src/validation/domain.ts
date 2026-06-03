// validation/domain.ts — Custom validators for the domain DSL.
//
// The `global.` prefix is reserved for embedded LLM bootstrap
// prompts in oos.global_prompt — domain rows colliding with it
// would corrupt the joint vector space oosai uses for retrieval.
//
// Note: the current grammar's ID terminal (`[_a-zA-Z][\w_]*`) does
// not actually permit `.` in a domain name, so the lexer rejects
// `global.foo` before this validator ever runs. The check stays
// as a belt-and-suspenders guard in case the grammar later allows
// dotted identifiers (namespaced domain names, for instance), and
// it gives a clearer error message than the bare lexer complaint
// for any future grammar version.
//
// Adding more checks: write a method on DomainValidator and add
// an entry to the ValidationChecks map in domainChecks() below.

import type { ValidationAcceptor, ValidationChecks } from "langium";

import type { AliasList, DomainDecl, OnisinAstType } from "../generated/ast.js";

/** Container for all domain-level validation checks. */
export class DomainValidator {
	/**
	 * Reject domain names that begin with the reserved `global.`
	 * prefix. The diagnostic points at the name token specifically
	 * so the underline lands on the offending text rather than the
	 * whole declaration.
	 */
	checkGlobalPrefix(decl: DomainDecl, accept: ValidationAcceptor): void {
		if (decl.name.startsWith("global.")) {
			accept(
				"error",
				"Domain names starting with 'global.' are reserved for embedded LLM prompts.",
				{ node: decl, property: "name" },
			);
		}
	}

	/**
	 * Warn on `aliases [...]` clauses that contribute nothing — an
	 * empty list, or one whose entries are all whitespace. The
	 * mapper drops blanks silently, so this is a soft warning, not
	 * an error: the parse stays valid and the rest of the domain
	 * keeps working.
	 */
	checkAliasListNonEmpty(node: AliasList, accept: ValidationAcceptor): void {
		const meaningful = node.values.filter((v) => v.trim().length > 0);
		if (meaningful.length === 0) {
			accept(
				"warning",
				"Empty 'aliases [...]' clause has no effect; remove it or add entries.",
				{ node },
			);
		}
	}
}

/**
 * Build the ValidationChecks map for the domain language. Wired
 * up by the domain module override in services.ts.
 */
export function domainChecks(
	validator: DomainValidator,
): ValidationChecks<OnisinAstType> {
	return {
		DomainDecl: validator.checkGlobalPrefix.bind(validator),
		AliasList: validator.checkAliasListNonEmpty.bind(validator),
	};
}
