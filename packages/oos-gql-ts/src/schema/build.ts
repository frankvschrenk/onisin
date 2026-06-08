// schema/build.ts — Top-level GraphQLSchema assembly.
//
// Takes a list of DomainDef plus a DbClient and produces a complete
// schema with one Query and one Mutation type.
// Each domain contributes:
//
//   - one Query field         `<domain>` returning Type or [Type]
//   - three Mutation fields   `update_<domain>` / `insert_<domain>`
//                             / `delete_<domain>` returning Type
//   - one meta Query field    `meta_<name>` per declared Meta,
//                             returning [{value, label}]
//
// The function is pure with respect to its inputs: same DomainDef[]
// plus same Sql produces the same schema, every time. Hot-reloading
// on DSL changes is the caller's job — the Hono app wraps a
// `pg_notify` listener around this and rebuilds when the
// .domain table changes.
//
// Naming and operator suffixes are sourced from `naming.ts` and from
// `oos-dsls-ts`'s `operatorsForType`, the same module the LLM-chunk
// renderer reads. One source of truth for what arguments the schema
// accepts vs what arguments the LLM is taught to send.

import {
	GraphQLList,
	GraphQLObjectType,
	GraphQLSchema,
	type GraphQLFieldConfig,
	type GraphQLFieldConfigMap,
	type GraphQLObjectType as GQLObjectType,
	type GraphQLSchemaConfig,
} from "graphql";
import type { DbClient } from "../db-client";

import type { DomainDef, MetaDef } from "oos-dsls-ts";

import {
	domainQueryName,
	metaQueryName,
	mutationFieldName,
} from "../naming";
import {
	makeDeleteResolver,
	makeInsertResolver,
	makeQueryResolver,
	makeUpdateResolver,
} from "../resolvers/sql-resolvers";
import { makeMetaResolver } from "../resolvers/meta-resolver";

import { buildDomainObjectType, buildMetaObjectType } from "./object-type";
import { buildQueryArgs } from "./filter-args";
import { buildMutationArgs, buildInsertArgs, buildDeleteArgs } from "./mutation-args";

/**
 * buildSchema produces a GraphQLSchema covering every domain in the
 * argument list. Returns an empty schema (no Query / no Mutation)
 * when the input is empty — caller checks `schema.getQueryType()`
 * before exposing it.
 */
export function buildSchema(domains: DomainDef[], db: DbClient): GraphQLSchema {
	const queryFields: GraphQLFieldConfigMap<unknown, unknown> = {};
	const mutationFields: GraphQLFieldConfigMap<unknown, unknown> = {};

	for (const domain of domains) {
		const objType = buildDomainObjectType(domain);
		addDomainQuery(queryFields, domain, objType, db);
		addDomainMutations(mutationFields, domain, objType, db);
		addMetaQueries(queryFields, domain.metas, db);
	}

	if (Object.keys(queryFields).length === 0) {
		return new GraphQLSchema({});
	}

	// Build the config in a single literal so the readonly `mutation`
	// property gets set at construction. Conditionally including the
	// mutation key when there are mutation fields keeps the schema
	// validator happy when no domain produces mutations.
	const queryObj = new GraphQLObjectType({
		name: "Query",
		fields: queryFields,
	});
	const mutationObj =
		Object.keys(mutationFields).length > 0
			? new GraphQLObjectType({ name: "Mutation", fields: mutationFields })
			: undefined;
	const config: GraphQLSchemaConfig = mutationObj
		? { query: queryObj, mutation: mutationObj }
		: { query: queryObj };
	return new GraphQLSchema(config);
}

/**
 * addDomainQuery wires the top-level domain query, returning a list
 * type in the general case. The runtime resolver collapses to the
 * first row when an `id` arg is supplied — that mirrors the legacy
 * Go resolver.
 */
function addDomainQuery(
	fields: GraphQLFieldConfigMap<unknown, unknown>,
	domain: DomainDef,
	objType: GQLObjectType,
	db: DbClient,
): void {
	const cfg: GraphQLFieldConfig<unknown, unknown> = {
		type: new GraphQLList(objType),
		args: buildQueryArgs(domain),
		resolve: makeQueryResolver(domain, db),
	};
	fields[domainQueryName(domain.name)] = cfg;
}

/**
 * addDomainMutations wires update/insert/delete for one domain. All
 * three return the domain object type so callers can read back the
 * post-mutation row, including server-assigned columns like id and
 * timestamps.
 */
function addDomainMutations(
	fields: GraphQLFieldConfigMap<unknown, unknown>,
	domain: DomainDef,
	objType: GQLObjectType,
	db: DbClient,
): void {
	fields[mutationFieldName("update", domain.name)] = {
		type: objType,
		args: buildMutationArgs(domain),
		resolve: makeUpdateResolver(domain, db),
	};
	fields[mutationFieldName("insert", domain.name)] = {
		type: objType,
		args: buildInsertArgs(domain),
		resolve: makeInsertResolver(domain, db),
	};
	fields[mutationFieldName("delete", domain.name)] = {
		type: objType,
		args: buildDeleteArgs(),
		resolve: makeDeleteResolver(domain, db),
	};
}

/** addMetaQueries wires every declared meta as its own top-level query. */
function addMetaQueries(
	fields: GraphQLFieldConfigMap<unknown, unknown>,
	metas: MetaDef[],
	db: DbClient,
): void {
	for (const meta of metas) {
		const objType = buildMetaObjectType(meta);
		fields[metaQueryName(meta.name)] = {
			type: new GraphQLList(objType),
			resolve: makeMetaResolver(meta, db),
		};
	}
}
