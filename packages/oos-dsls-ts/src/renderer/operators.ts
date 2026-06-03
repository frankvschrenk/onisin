// renderer/operators.ts — Type-driven filter-operator catalog.
//
// Direct port of `operatorsForType()` and friends from
// `oosp/pluginsrv/store/schema_chunk.go`. The sample values mirror
// the originals so chunks generated from the new DSL look the same
// to a downstream embedding model — no behavioural drift on the
// vector side of the system.

import type { ExampleOpDef, FieldTypeDef } from "../types";

/** One GraphQL filter operator for a given field type. */
export interface FilterOp {
	/** Author-facing name (`like`, `eq`, `gt`, ...). */
	name: ExampleOpDef;
	/** Human-readable label used in the rendered "Filter example (X label)" header. */
	label: string;
	/** GraphQL argument suffix appended to the field name (`_contains`, `_eq`, ...). */
	suffix: string;
	/** Default value for the typed-defaults pass when the field has no overrides. */
	sampleValue: string;
}

/**
 * operatorsForType returns the operators we generate filter examples
 * for, given a field type. Wider operator coverage means more shapes
 * the LLM can copy verbatim.
 */
export function operatorsForType(fieldType: FieldTypeDef): FilterOp[] {
	switch (fieldType) {
		case "string":
		case "text":
			return [
				{ name: "like", label: "contains", suffix: "_contains", sampleValue: '"a"' },
				{ name: "eq", label: "equals", suffix: "_eq", sampleValue: '"value"' },
				{ name: "ne", label: "not equals", suffix: "_ne", sampleValue: '"value"' },
			];
		case "int":
		case "float":
			return [
				{ name: "eq", label: "equals", suffix: "_eq", sampleValue: "0" },
				{ name: "ne", label: "not equals", suffix: "_ne", sampleValue: "0" },
				{ name: "gt", label: "greater than", suffix: "_gt", sampleValue: "0" },
				{ name: "ge", label: "greater or equal", suffix: "_gte", sampleValue: "0" },
				{ name: "lt", label: "less than", suffix: "_lt", sampleValue: "0" },
				{ name: "le", label: "less or equal", suffix: "_lte", sampleValue: "0" },
			];
		case "bool":
			return [{ name: "eq", label: "equals", suffix: "_eq", sampleValue: "true" }];
		case "date":
		case "datetime":
			return [
				{ name: "eq", label: "equals", suffix: "_eq", sampleValue: '"2024-01-01"' },
				{ name: "gt", label: "after", suffix: "_gt", sampleValue: '"2024-01-01"' },
				{ name: "lt", label: "before", suffix: "_lt", sampleValue: '"2024-01-01"' },
			];
		default:
			return [];
	}
}

/**
 * findOperator returns the FilterOp matching the user-supplied op
 * name for a given field type. Used when an Example block overrides
 * a typed default with a concrete value.
 */
export function findOperator(
	fieldType: FieldTypeDef,
	opName: ExampleOpDef,
): FilterOp | undefined {
	return operatorsForType(fieldType).find((op) => op.name === opName);
}

/**
 * formatExampleValue quotes string values and leaves numeric/boolean
 * values bare. Mirrors the Go renderer's behaviour where the author
 * supplies the literal exactly as it should appear in the GraphQL
 * argument, except strings always get quoted.
 */
export function formatExampleValue(
	fieldType: FieldTypeDef,
	value: string,
	valueIsString: boolean,
): string {
	if (valueIsString) return `"${value}"`;
	// For string/text fields, even a numeric-looking value should be
	// quoted because the GraphQL argument is typed as String.
	if (fieldType === "string" || fieldType === "text") return `"${value}"`;
	return value;
}

/** renderFilterArg formats a single GraphQL filter argument. */
export function renderFilterArg(
	fieldName: string,
	suffix: string,
	value: string,
): string {
	return `${fieldName}${suffix}: ${value}`;
}
