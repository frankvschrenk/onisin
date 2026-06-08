import type {
    PipelineDef,
    StepLlm,
    StepSemantic,
    StepWhere,
} from './types.js';

/** A single validation diagnostic. */
export interface PipelineDiagnostic {
    severity: 'error' | 'warning';
    message:  string;
    /** Step or source name involved, when applicable. */
    ref?:     string;
}

/**
 * validatePipeline checks semantic rules that the Langium grammar cannot
 * express, such as cross-step reference ordering and mode constraints.
 *
 * Returns an empty array when the pipeline is valid.
 */
export function validatePipeline(def: PipelineDef): PipelineDiagnostic[] {
    const diags: PipelineDiagnostic[] = [];

    const stepNames   = new Set(def.steps.map(s => s.name));
    const docSources  = new Set(def.sources.filter(s => s.kind === 'docs').map(s => s.name));
    const dbSources   = new Set(def.sources.filter(s => s.kind === 'db').map(s => s.name));

    // ── mode constraints ───────────────────────

    const semanticSteps = def.steps.filter(s => s.kind === 'semantic') as StepSemantic[];

    if (def.mode === 'mass' && semanticSteps.length === 0) {
        diags.push({
            severity: 'error',
            message: 'Pipeline mode "mass" requires at least one semantic step for vector pre-selection.',
        });
    }

    // ── out=nats requires subject ────────────────

    if (def.out.target === 'nats' && !def.out.subject) {
        diags.push({
            severity: 'error',
            message: 'Output target "nats" requires a subject.',
        });
    }

    // ── per-step validation ──────────────────

    for (let i = 0; i < def.steps.length; i++) {
        const step = def.steps[i];

        if (step.kind === 'semantic') {
            const s = step as StepSemantic;

            // Vector search operates on a db vector column. A docs source has no
            // such column, so semantic may only target a db source or a step.
            if (docSources.has(s.source)) {
                diags.push({
                    severity: 'error',
                    message: `Step "${s.name}": semantic steps operate on a db vector column, not a docs source.`,
                    ref: s.name,
                });
            } else {
                // source must be a known db source OR a preceding step.
                const srcIdx    = def.steps.findIndex(t => t.name === s.source);
                const isDbName  = dbSources.has(s.source);
                if (!isDbName) {
                    if (srcIdx < 0) {
                        diags.push({
                            severity: 'error',
                            message: `Step "${s.name}": source "${s.source}" is not a known step or db source.`,
                            ref: s.name,
                        });
                    } else if (srcIdx >= i) {
                        diags.push({
                            severity: 'error',
                            message: `Step "${s.name}": source "${s.source}" must be a preceding step.`,
                            ref: s.name,
                        });
                    }
                }
            }

            if (!def.embedding) {
                diags.push({
                    severity: 'error',
                    message: `Step "${s.name}": semantic steps require an embedding model declaration.`,
                    ref: s.name,
                });
            }

            if (s.queries.length === 0) {
                diags.push({
                    severity: 'error',
                    message: `Step "${s.name}": at least one query is required.`,
                    ref: s.name,
                });
            }
        }

        if (step.kind === 'where') {
            const s = step as StepWhere;
            if (!dbSources.has(s.source)) {
                diags.push({
                    severity: 'error',
                    message: `Step "${s.name}": where steps may only reference a db source.`,
                    ref: s.name,
                });
            }
        }

        if (step.kind === 'llm') {
            const llm = step as StepLlm;

            // source must be a preceding step OR a direct db/docs source.
            const srcIdx    = def.steps.findIndex(t => t.name === llm.source);
            const isSrcName = def.sources.some(s => s.name === llm.source);
            if (!isSrcName) {
                if (srcIdx < 0) {
                    diags.push({
                        severity: 'error',
                        message: `Step "${llm.name}": source "${llm.source}" is not a known step or source.`,
                        ref: llm.name,
                    });
                } else if (srcIdx >= i) {
                    diags.push({
                        severity: 'error',
                        message: `Step "${llm.name}": source "${llm.source}" must be a preceding step.`,
                        ref: llm.name,
                    });
                }
            }

            if (llm.input !== undefined) {
                if (!stepNames.has(llm.input)) {
                    diags.push({
                        severity: 'error',
                        message: `Step "${llm.name}": input "${llm.input}" references an unknown step.`,
                        ref: llm.name,
                    });
                } else if (llm.input === llm.name) {
                    diags.push({
                        severity: 'error',
                        message: `Step "${llm.name}": input must not reference itself.`,
                        ref: llm.name,
                    });
                } else {
                    const inputIndex = def.steps.findIndex(s => s.name === llm.input);
                    if (inputIndex >= i) {
                        diags.push({
                            severity: 'error',
                            message: `Step "${llm.name}": input "${llm.input}" must be a preceding step.`,
                            ref: llm.name,
                        });
                    }
                }
            }
        }
    }

    // ── source name uniqueness ─────────────────────

    const seenSources = new Set<string>();
    for (const src of def.sources) {
        if (seenSources.has(src.name)) {
            diags.push({ severity: 'error', message: `Duplicate source name "${src.name}".`, ref: src.name });
        }
        seenSources.add(src.name);
    }

    // ── step name uniqueness ───────────────────

    const seenSteps = new Set<string>();
    for (const step of def.steps) {
        if (seenSteps.has(step.name)) {
            diags.push({ severity: 'error', message: `Duplicate step name "${step.name}".`, ref: step.name });
        }
        seenSteps.add(step.name);
    }

    return diags;
}
