// index.ts — Public API of oos-gql-ts.
//
// Two surfaces:
//
//   1. `buildSchema(domains, db)` — the main entry point. Hand it a
//      list of DomainDef and a DbClient, get back a
//      ready-to-execute GraphQLSchema.
//
//   2. Permission helpers — `isActionAllowed` / `assertActionAllowed`.
//      Pure functions, callable without a SQL connection. The Hono
//      app uses them as the auth gate; the editor uses them for
//      preview-side capability checks.
//
//   3. Client-side query / mutation builders — `buildMutationFromMap`
//      for insert/update, `buildDeleteMutation` for delete,
//      `buildMetaQueriesForDomain` for fetching dropdown options.
//      Used by the LLM tool loop and the detail tab in apps/oos to
//      translate a save / delete / fetch-options intent into a
//      GraphQL string ready to POST to oosgql.
//
// Naming helpers and the underlying SQL builders are exported as
// well, but as advanced surface — most consumers shouldn't need
// them. They're public mainly so the future oosgql HTTP layer can
// translate raw query strings into permission lookups without
// re-implementing the parsing.

export { buildSchema } from "./schema/build";
export type { DbClient, DbDialect, DbRow } from "./db-client";
export { isActionAllowed, assertActionAllowed } from "./permissions";

// Client-side mutation rendering — input for the LLM tool loop and
// for the detail-tab Save / Delete buttons in apps/oos.
export { buildMutationFromMap, buildDeleteMutation } from "./client/build-mutation";
export type { BuildMutationResult } from "./client/build-mutation";

// Client-side meta-query rendering — used by the detail tab to load
// dropdown options in one round-trip.
export { buildMetaQueriesForDomain, extractMetaPayload } from "./client/build-meta-query";
export { buildDetailQuery, extractDetailRow } from "./client/build-detail-query";

// Naming conventions — exported for callers that need to construct
// query / mutation names outside the schema (e.g. permission lookups
// keyed on a parsed mutation field name).
export {
	domainQueryName,
	domainTypeName,
	mutationFieldName,
	metaQueryName,
	metaTypeName,
} from "./naming";

// Operator suffix and SQL builders — advanced API for tools that want
// to inspect what the schema will accept without building it.
export { buildWhereClause, selectColumnList } from "./sql/select";
export { buildUpdate, buildInsert, buildDelete } from "./sql/mutation";
