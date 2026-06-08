// mapper.ts — converts the Langium AST to plain PipelineDef runtime types.
// Import only after langium generate has been run.

import type {
    PipelineDef    as AstPipelineDef,
    StepSemantic   as AstStepSemantic,
    StepLlm        as AstStepLlm,
    StepWhere      as AstStepWhere,
    SourceDb       as AstSourceDb,
    SourceDocs     as AstSourceDocs,
    WhereExpr      as AstWhereExpr,
    FieldFilter    as AstFieldFilter,
    AndExpr        as AstAndExpr,
    OrExpr         as AstOrExpr,
    NotExpr        as AstNotExpr,
    FilterValue    as AstFilterValue,
    SemanticQuery  as AstSemanticQuery,
} from './generated/ast.js';

import type {
    PipelineDef,
    Source,
    Step,
    OutDef,
    WhereExpr,
    FilterValue,
} from './types.js';

/** Maps the Langium AST root node to a plain PipelineDef. */
export function mapPipeline(ast: AstPipelineDef): PipelineDef {
    return {
        name:      stripQuotes(ast.name),
        sources:   ast.sources.map(mapSource),
        mode:      ast.mode.value as PipelineDef['mode'],
        llm:       stripQuotes(ast.llm.model),
        embedding: ast.embedding ? stripQuotes(ast.embedding.model) : undefined,
        steps:     ast.steps.map(mapStep),
        out:       mapOut(ast.out),
    };
}

// ── Sources ──────────────────────────────────────

function mapSource(src: AstSourceDb | AstSourceDocs): Source {
    if (src.$type === 'SourceDb') {
        const s = src as AstSourceDb;
        return {
            kind:  'db',
            name:  s.name,
            table: s.table,
            dsn:   s.dsn ? stripQuotes(s.dsn) : undefined,
        };
    }
    const s = src as AstSourceDocs;
    return {
        kind: 'docs',
        name: s.name,
        uri:  stripQuotes(s.uri),
    };
}

// ── Steps ──────────────────────────────────

function mapStep(step: AstStepSemantic | AstStepWhere | AstStepLlm): Step {
    switch (step.$type) {
        case 'StepSemantic': {
            const s = step as AstStepSemantic;
            return {
                kind:    'semantic',
                name:    s.name,
                source:  s.source,
                where:   s.where ? mapWhereExpr(s.where) : undefined,
                queries: s.queries.map((q: AstSemanticQuery) => stripQuotes(q.value)),
                limit:   s.limit,
            };
        }
        case 'StepWhere': {
            const s = step as AstStepWhere;
            return {
                kind:   'where',
                name:   s.name,
                source: resolveRefName(s.source),
                limit:  s.limit,
                where:  s.where ? mapWhereExpr(s.where) : undefined,
            };
        }
        case 'StepLlm': {
            const s = step as AstStepLlm;
            return {
                kind:   'llm',
                name:   s.name,
                source: s.source,
                input:  s.input?.ref?.name,
                mode:   s.mode as 'per-row' | 'aggregate',
                system: s.system ? stripQuotes(s.system) : undefined,
                prompt: stripQuotes(s.prompt),
            };
        }
    }
}

// ── Where expression (Hasura-style) ────────────────────────────────────

function mapWhereExpr(expr: AstWhereExpr): WhereExpr {
    switch (expr.$type) {
        case 'AndExpr':
            return { kind: 'and', exprs: (expr as AstAndExpr).exprs.map(mapWhereExpr) };
        case 'OrExpr':
            return { kind: 'or',  exprs: (expr as AstOrExpr).exprs.map(mapWhereExpr) };
        case 'NotExpr':
            return { kind: 'not', expr: mapWhereExpr((expr as AstNotExpr).expr) };
        case 'FieldFilter': {
            const f = expr as AstFieldFilter;
            return {
                kind:  'field',
                field: f.field,
                op:    f.op as import('./types.js').FilterOp,
                value: mapFilterValue(f.value),
            };
        }
    }
    // Should never reach here if grammar is correct.
    throw new Error(`Unknown WhereExpr type: ${(expr as { $type: string }).$type}`);
}

function mapFilterValue(v: AstFilterValue): FilterValue {
    if (v.$type === 'StringValue') {
        return { kind: 'string', value: stripQuotes((v as { value: string }).value) };
    }
    if (v.$type === 'NumberValue') {
        return { kind: 'number', value: (v as { value: number }).value };
    }
    // ParamRef
    return { kind: 'param', param: (v as { param: string }).param };
}

// ── Output ──────────────────────────────────

function mapOut(out: AstPipelineDef['out']): OutDef {
    return {
        target:  out.target as OutDef['target'],
        subject: out.subject ? stripQuotes(out.subject) : undefined,
    };
}

/**
 * Resolves a Langium cross-reference to the referenced node's name.
 * Falls back to $refText when the reference is unresolved
 * (the validator will report it separately as a diagnostic).
 */
function resolveRefName(ref: { ref?: { name: string }; $refText: string }): string {
    return ref.ref?.name ?? ref.$refText;
}

/** Removes surrounding double or single quotes from a STRING terminal value. */
function stripQuotes(s: string): string {
    if ((s.startsWith('"') && s.endsWith('"')) ||
        (s.startsWith("'") && s.endsWith("'"))) {
        return s.slice(1, -1);
    }
    return s;
}
