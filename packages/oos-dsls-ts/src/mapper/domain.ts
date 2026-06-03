// mapper/domain.ts — AST → DomainDef mapper.
//
// The single public entry point is `mapDomain()`. It receives the
// parsed `DomainModel` (the root AST node of a `.domain` file) and
// returns the runtime DomainDef consumed by the LLM-chunk renderer
// and any future GraphQL schema builder.
//
// Field modifiers are pre-bucketed here so the renderer never has
// to walk the heterogenous AST modifier union. Cross-references
// (e.g. `OptionsRef.meta`) are stored by textual name only — the
// actual Meta target is looked up in the same DomainDef during
// rendering, not at mapping time, so a missing meta does not abort
// the parse.

import type {
	AiHint,
	AliasList,
	DomainModel,
	Example,
	Field,
	FieldModifier,
	Meta,
	NumberValue,
	Permission,
	Relation,
	StringValue,
} from "../generated/ast";
import type {
	AiHintDef,
	DomainDef,
	DomainFieldDef,
	ExampleDef,
	ExampleOpDef,
	FieldTypeDef,
	MetaDef,
	PermissionActionDef,
	PermissionDef,
	RelationDef,
	RelationKindDef,
} from "../types";

/**
 * Map a parsed DomainModel to a runtime DomainDef.
 *
 * The mapper is total: every input produces a DomainDef. Members
 * dispatch by `$type`; unrecognised members (a future grammar
 * addition) are dropped silently so older mapper builds keep working
 * against newer grammars.
 */
export function mapDomain(model: DomainModel): DomainDef {
	const decl = model.domain;

	const fields: DomainFieldDef[] = [];
	const permissions: PermissionDef[] = [];
	const relations: RelationDef[] = [];
	const metas: MetaDef[] = [];
	const aiHints: AiHintDef[] = [];
	const aliases: string[] = [];

	for (const member of decl.members) {
		switch (member.$type) {
			case "Field":
				fields.push(mapField(member as Field));
				break;
			case "Permission":
				permissions.push(mapPermission(member as Permission));
				break;
			case "Relation":
				relations.push(mapRelation(member as Relation));
				break;
			case "Meta":
				metas.push(mapMeta(member as Meta));
				break;
			case "AiHint":
				aiHints.push(mapAiHint(member as AiHint));
				break;
			case "AliasList":
				// Multiple clauses concatenate; empty values are
				// dropped here so downstream consumers never see
				// blanks even if the validator missed them.
				for (const v of (member as AliasList).values) {
					const trimmed = v.trim();
					if (trimmed.length > 0) aliases.push(trimmed);
				}
				break;
		}
	}

	return {
		name: decl.name,
		source: decl.source,
		dsn: decl.dsn,
		permissions,
		fields,
		relations,
		metas,
		aiHints,
		aliases,
	};
}

/**
 * mapField pre-buckets modifiers and example blocks so renderer code
 * does not need to walk the AST union at every access. Unknown
 * modifier kinds are ignored, mirroring the defensive style used in
 * `mapper/common.ts`.
 */
function mapField(f: Field): DomainFieldDef {
	let readOnly = false;
	let filterable = false;
	let optionsRef: string | undefined;

	for (const mod of f.modifiers) {
		const m = mod as FieldModifier;
		switch (m.$type) {
			case "ReadOnly":
				readOnly = true;
				break;
			case "Filterable":
				filterable = true;
				break;
			case "OptionsRef":
				// Cross-reference. Use the textual name (`$refText`),
				// which Langium always populates even when the target
				// is unresolved. The renderer treats a dangling
				// optionsRef as "no meta" and silently drops it.
				optionsRef = m.meta?.$refText;
				break;
		}
	}

	return {
		name: f.name,
		type: f.type as FieldTypeDef,
		readOnly,
		filterable,
		optionsRef,
		examples: f.examples.map(mapExample),
	};
}

/** mapExample stringifies the typed value and remembers its origin. */
function mapExample(ex: Example): ExampleDef {
	const v = ex.value;
	if (v.$type === "StringValue") {
		const s = v as StringValue;
		return {
			op: ex.op as ExampleOpDef,
			value: s.value,
			valueIsString: true,
			description: ex.description,
		};
	}
	const n = v as NumberValue;
	return {
		op: ex.op as ExampleOpDef,
		value: String(n.value),
		valueIsString: false,
		description: ex.description,
	};
}

function mapPermission(p: Permission): PermissionDef {
	return {
		role: p.role,
		actions: p.actions.map((a) => a as PermissionActionDef),
	};
}

function mapRelation(r: Relation): RelationDef {
	return {
		name: r.name,
		kind: r.kind as RelationKindDef,
		target: r.target,
		localField: r.localField,
		foreignField: r.foreignField,
	};
}

function mapMeta(m: Meta): MetaDef {
	return {
		name: m.name,
		table: m.table,
		valueField: m.valueField,
		labelField: m.labelField,
		orderBy: m.orderBy,
		dsn: m.dsn,
	};
}

function mapAiHint(h: AiHint): AiHintDef {
	return { name: h.name, body: h.body };
}
