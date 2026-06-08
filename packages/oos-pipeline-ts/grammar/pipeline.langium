grammar Pipeline

// ── Entry ────────────────────────────────────────────────────────

entry PipelineDef:
    'pipeline' name=STRING '{'
        sources+=Source+
        mode=ModeDecl
        llm=LlmDecl
        (embedding=EmbeddingDecl)?
        steps+=Step+
        out=OutDecl
    '}'
;

// ── Sources ──────────────────────────────────────────────────────

Source:
    SourceDb | SourceDocs
;

// source db schaeden from pipeline_documents
SourceDb:
    'source' 'db' name=ID 'from' table=ID ('dsn=' dsn=STRING)?
;

// source docs schaedenDocs uri="s3://schaeden/"
// Reserved for the document track (OpenDAL list+read, no vectors): the URI
// scheme selects the storage backend (s3://, gs://, az://, file:// ...). Parsed
// today but not yet consumed by any step; wired up when the document picker lands.
SourceDocs:
    'source' 'docs' name=ID 'uri=' uri=STRING
;

// ── Global Config ─────────────────────────────────────────────────

ModeDecl:
    'mode' value=PipelineMode
;

PipelineMode returns string:
    'context' | 'mass'
;

LlmDecl:
    'llm' model=STRING
;

EmbeddingDecl:
    'embedding' model=STRING
;

// ── Steps ─────────────────────────────────────────────────────────

Step:
    StepWhere | StepSemantic | StepLlm
;

// StepWhere: structured filter on a db source using a Hasura-style where block.
// Supports _and, _or, _not for boolean logic. Compiles to dialect SQL.
// Example:
//   step where vorauswahl {
//     from schaeden
//     where {
//       _and: {
//         { kategorie: { _eq: "Einbruch" } }
//         { schadenshoehe: { _gt: 10000 } }
//       }
//     }
//   }
StepWhere:
    'step' 'where' name=ID '{'
        'from' source=[SourceDb:ID]
        (
            ('limit' limit=INT) |
            ('where' '{' where=WhereExpr '}')
        )*
    '}'
;

// StepSemantic: vector similarity search (top-K by similarity to one or more
// queries) over a db source's vector column. Engine-agnostic at the DSL level;
// the runtime emits dialect SQL for the distance operator (<=> on pgvector,
// VECTOR_DISTANCE on Oracle), embedding the query via oosai.
//
// `from` references either a db source directly or a preceding step by name.
// Referencing a preceding step narrows the candidate set to that step's rows
// (AND semantics). Multiple `query` lines within one step are ORed (their hits
// are unioned). `query` is mandatory — a similarity search without a query
// vector is meaningless, so there is deliberately no step-name fallback.
StepSemantic:
    'step' 'semantic' name=ID '{'
        'from' source=ID
        ('where' '{' where=WhereExpr '}')?
        queries+=SemanticQuery+
        ('limit' limit=INT)?
    '}'
;

SemanticQuery:
    'query' value=STRING
;

// ── Where Expression (Hasura-style) ────────────────────────────────────

WhereExpr:
    AndExpr | OrExpr | NotExpr | FieldFilter
;

// _and: { expr expr ... }
AndExpr:
    '_and' ':' '{'
        exprs+=WhereExpr+
    '}'
;

// _or: { expr expr ... }
OrExpr:
    '_or' ':' '{'
        exprs+=WhereExpr+
    '}'
;

// _not: { expr }
NotExpr:
    '_not' ':' '{' expr=WhereExpr '}'
;

// { field: { _op: value } }
FieldFilter:
    '{' field=ID ':' '{' op=FilterOp ':' value=FilterValue '}' '}'
;

FilterOp returns string:
    '_eq' | '_neq' | '_gt' | '_gte' | '_lt' | '_lte' | '_like' | '_ilike' | '_in'
;

// A filter value is either a quoted string, number, or :param reference.
FilterValue:
    StringValue | NumberValue | ParamRef
;

StringValue:
    value=STRING
;

NumberValue:
    value=NUMBER
;

ParamRef:
    ':' param=ID
;

// StepLlm: LLM analysis step, per-row or aggregate.
StepLlm:
    'step' 'llm' name=ID '{'
        'from'   source=ID
        ('input' input=[Step:ID])?
        'mode'   mode=StepMode
        ('system' system=STRING)?
        'prompt' prompt=STRING
    '}'
;

StepMode returns string:
    'per-row' | 'aggregate'
;

// ── Output ──────────────────────────────────────────────────────

OutDecl:
    'out' target=OutTarget
    ('subject' subject=STRING)?
;

OutTarget returns string:
    'editor' | 'nats' | 'file'
;

// ── Terminals ───────────────────────────────────────────────────

terminal ID:               /[_a-zA-Z][\w-]*/;
terminal INT    returns number: /[0-9]+/;
terminal NUMBER returns number: /-?[0-9]+(\.[0-9]+)?/;
terminal STRING:           /"[^"]*"|'[^']*'/;
hidden terminal WS:         /\s+/;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
hidden terminal ML_COMMENT: /\/\*[\s\S]*?\*\//;
