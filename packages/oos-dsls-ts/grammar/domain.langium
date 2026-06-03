// domain.langium — Grammar for the onisin domain-dsl.
//
// A domain describes a data model: fields with types, permissions,
// relations to other domains, lookup sources for dropdowns, filter
// hints for an LLM, and free-form AI hints. UI behaviour lives in
// the view-dsl, never here.
//
// Design decisions taken in this revision:
//
//   * 'label' was a keyword in the older meta rule and conflicted
//     with downstream view-dsl elements. The meta rule now uses
//     positional fields: 'meta NAME from TABLE VALUE_FIELD LABEL_FIELD'
//     plus optional 'order_by ID' and 'via ID'.
//
//   * Numbers use a single NUMBER terminal.
//
//   * AI hint bodies use STRING (with triple-quoted multiline
//     support could be added later); writing free text inside
//     {...} confused the lexer too easily.
//
// File extension: .domain
// One domain per file.

grammar Domain

entry DomainModel:
    domain=DomainDecl;

DomainDecl:
    'domain' name=ID 'from' source=ID '@' dsn=ID '{'
        members+=DomainMember*
    '}';

DomainMember:
    Permission | Field | Relation | Meta | AiHint | AliasList;

// ─── Permissions ────────────────────────────────────────────────

Permission:
    'permission' role=ID actions+=PermissionAction (',' actions+=PermissionAction)*;

PermissionAction returns string:
    'read' | 'write' | 'delete';

// ─── Fields ─────────────────────────────────────────────────────

Field:
    'field' name=ID ':' type=FieldType
    modifiers+=FieldModifier*
    ('{' examples+=Example* '}')?;

FieldType returns string:
    'int' | 'float' | 'string' | 'text' | 'bool' | 'date' | 'datetime';

FieldModifier:
      {infer ReadOnly}    'readonly'
    | {infer Filterable}  'filterable'
    | {infer OptionsRef}  'options' '=' meta=[Meta:ID];

Example:
    'example' op=ExampleOp value=ExampleValue description=STRING;

ExampleOp returns string:
    'eq' | 'ne' | 'lt' | 'le' | 'gt' | 'ge' | 'like' | 'in';

// Examples accept either a string ("Anna") or a numeric literal.
ExampleValue:
      {infer StringValue} value=STRING
    | {infer NumberValue} value=NUMBER;

// ─── Relations ──────────────────────────────────────────────────

Relation:
    'relation' name=ID ':' kind=RelationKind target=ID
    'bind' '=' localField=ID '->' foreignField=ID;

RelationKind returns string:
    'has_many' | 'has_one' | 'belongs_to';

// ─── Meta sources (lookup tables) ───────────────────────────────
// 'meta NAME from TABLE VALUE_FIELD LABEL_FIELD [order_by FIELD] [via DSN]'
//
// Reads positionally so 'value' and 'label' do not have to become
// keywords (which would clash with field names downstream).

Meta:
    'meta' name=ID 'from' table=ID
    valueField=ID labelField=ID
    ('order_by' orderBy=ID)?
    ('via' dsn=ID)?;

// ─── AI hints ───────────────────────────────────────────────────

AiHint:
    'ai' name=STRING body=STRING;

// ─── Aliases ────────────────────────────────────────────────────
// 'aliases [ "personen", "mitarbeiter", "kunden" ]'
//
// Author-supplied alternative names for the domain. The pre-LLM
// resolver in apps/oos uses these to map free-text user input
// ("zeig mir alle Mitarbeiter") to a domain pin without going
// through the LLM. The plural-form heuristic in
// renderer/aliases.ts keeps running for domains that omit this
// clause; when present the explicit list is appended on top, so
// regular plurals stay covered while irregular plurals, English
// borrowings and synonyms get a deterministic match.
//
// Multiple 'aliases [...]' clauses are concatenated by the mapper
// rather than rejected, mirroring the defensive style used for
// other repeating members.

AliasList:
    'aliases' '[' values+=STRING (',' values+=STRING)* ']';

// ─── Terminals ──────────────────────────────────────────────────

terminal ID: /[_a-zA-Z][\w_]*/;
terminal NUMBER returns number: /-?[0-9]+(\.[0-9]+)?/;
terminal STRING: /"(\\.|[^"\\])*"/;

hidden terminal WS: /\s+/;
hidden terminal ML_COMMENT: /\/\*[\s\S]*?\*\//;
hidden terminal SL_COMMENT: /\/\/[^\n\r]*/;
