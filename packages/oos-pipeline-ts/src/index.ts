// Public API for oos-pipeline-ts.
// Consumer-facing re-exports — runtime types, parser, validator.
// Generated AST types are intentionally not re-exported here;
// import from 'oos-pipeline-ts/generated' when you need the raw AST.

export type {
    PipelineDef,
    PipelineMode,
    Source,
    SourceDb,
    SourceDocs,
    Step,
    StepSemantic,
    StepWhere,
    StepLlm,
    StepMode,
    WhereExpr,
    FilterOp,
    FilterValue,
    ParamValues,
    OutDef,
    OutTarget,
} from './types.js';

export { parsePipeline } from './parser.js';
export type { ParsePipelineResult } from './parser.js';

export { validatePipeline } from './validator.js';
export type { PipelineDiagnostic } from './validator.js';

export { createOnisinPipelineServices } from './services.js';
