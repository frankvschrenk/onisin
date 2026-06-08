// Pipeline runtime types — plain TypeScript interfaces, no Langium AST dependency.
// These are used by the runner, validator, and any consumer outside the parser.

/** Top-level pipeline definition. */
export interface PipelineDef {
    name: string;
    sources: Source[];
    mode: PipelineMode;
    llm: string;
    embedding?: string;
    steps: Step[];
    out: OutDef;
}

/** Pipeline execution mode. */
export type PipelineMode = 'context' | 'mass';

// ── Sources ─────────────────────────────────────────────────

export type Source = SourceDb | SourceDocs;

/** A relational database source (PostgreSQL, Oracle, SQLite, etc.). */
export interface SourceDb {
    kind:   'db';
    name:   string;
    /** Table to query, e.g. pipeline_documents, person, police_incidents. */
    table: string;
    /** Optional DSN override. Falls back to environment default when absent. */
    dsn?:   string;
}

/**
 * A document source addressed by a storage URI. Reserved for the document track
 * (OpenDAL list+read, no vectors): the scheme selects the backend at runtime
 * (s3://, gs://, az://, file://, ...). Carried through the model today but not
 * yet consumed by any step; wired up when the document picker lands.
 */
export interface SourceDocs {
    kind: 'docs';
    name: string;
    /** Storage URI; the scheme selects the OpenDAL backend at runtime. */
    uri: string;
}

// ── Steps ────────────────────────────────────────────────

export type Step = StepWhere | StepSemantic | StepLlm;

/** Step mode used in StepLlm. */
export type StepMode = 'per-row' | 'aggregate';

// ── Where Expression types (Hasura-style) ────────────────────────────────────

export type FilterOp = '_eq' | '_neq' | '_gt' | '_gte' | '_lt' | '_lte' | '_like' | '_ilike' | '_in';

/**
 * Runtime values for :param references in the pipeline DSL.
 * Keys are the param names without the leading colon.
 */
export type ParamValues = Record<string, string | number>;

export type FilterValue =
    | { kind: 'string'; value: string }
    | { kind: 'number'; value: number }
    | { kind: 'param';  param: string };

export type WhereExpr =
    | { kind: 'and';   exprs: WhereExpr[] }
    | { kind: 'or';    exprs: WhereExpr[] }
    | { kind: 'not';   expr:  WhereExpr }
    | { kind: 'field'; field: string; op: FilterOp; value: FilterValue };

/**
 * Where step: structured filter on a db source using a Hasura-style where block.
 * Compiles to dialect SQL at runtime.
 */
export interface StepWhere {
    kind:    'where';
    name:    string;
    source:  string;
    limit?:  number;
    where?:  WhereExpr;
}

/**
 * Semantic step: vector similarity search over a db source's vector column.
 *
 * The DSL surface stays engine-agnostic; the runtime emits dialect SQL for the
 * distance operator and embeds the query via oosai. `source` names a db source
 * directly or a preceding step (chaining narrows the candidate set — AND).
 * Multiple `queries` are ORed (their hits are unioned). An optional `where`
 * pre-filters the candidate rows before the vector search.
 */
export interface StepSemantic {
    kind: 'semantic';
    name: string;
    /** Name of a db source or a preceding step to search. */
    source: string;
    /** Optional metadata pre-filter, applied before the vector search. */
    where?: WhereExpr;
    /** One or more query strings. Multiple queries produce a union. Required. */
    queries: string[];
    /** Maximum number of results to return. Defaults to 20 when absent. */
    limit?: number;
}

/**
 * LLM step: sends retrieved documents to the configured language model.
 * Operates either per-row (parallel) or aggregate (all rows at once).
 */
export interface StepLlm {
    kind: 'llm';
    name: string;
    /** Name of the preceding step or source to draw data from. */
    source: string;
    /** Name of a preceding step whose output is passed as additional context. */
    input?: string;
    mode: StepMode;
    /** Optional system prompt prepended before the user prompt. */
    system?: string;
    prompt: string;
}

// ── Output ────────────────────────────────────────────────────

export type OutTarget = 'editor' | 'nats' | 'file';

export interface OutDef {
    target: OutTarget;
    /** Required when target is "nats". */
    subject?: string;
}
