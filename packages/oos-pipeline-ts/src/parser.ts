// parser.ts — convenience wrapper around the Langium services for parsing
// a .pipeline source string into a validated PipelineDef.

import { NodeFileSystem } from 'langium/node';
import { URI } from 'langium';
import { createOnisinPipelineServices } from './services.js';
import { mapPipeline } from './mapper.js';
import { validatePipeline, type PipelineDiagnostic } from './validator.js';
import type { PipelineDef } from './types.js';

let _services: ReturnType<typeof createOnisinPipelineServices> | undefined;

function getServices() {
    if (!_services) {
        _services = createOnisinPipelineServices(NodeFileSystem);
        // Langium 4.x: services must be registered in the ServiceRegistry
        // so LangiumDocumentFactory can resolve the language by URI.
        const registry = _services.OnisinPipeline.shared.ServiceRegistry;
        registry.register(_services.OnisinPipeline);
    }
    return _services;
}

export interface ParsePipelineResult {
    def: PipelineDef | undefined;
    diagnostics: PipelineDiagnostic[];
}

/**
 * parsePipeline parses a .pipeline source string and returns the runtime
 * definition together with any diagnostics.
 *
 * Parse errors from Langium are included as diagnostics with severity "error".
 * Semantic diagnostics from validatePipeline are appended afterwards.
 */
export async function parsePipeline(
    source: string,
    uri = 'file:///pipeline/current.pipeline',
): Promise<ParsePipelineResult> {
    const services  = getServices();
    const documents = services.OnisinPipeline.shared.workspace.LangiumDocuments;
    const builder   = services.OnisinPipeline.shared.workspace.DocumentBuilder;
    const parsedUri = URI.parse(uri);

    // Defensive pre-cleanup: a previous call may have left a document
    // under this URI (parse error path, or thrown build). Re-adding the
    // same URI throws 'A document with the URI ... is already present'.
    if (documents.hasDocument(parsedUri)) {
        documents.deleteDocument(parsedUri);
    }

    const doc = services.OnisinPipeline.shared.workspace.LangiumDocumentFactory
        .fromString(source, parsedUri);

    documents.addDocument(doc);
    try {
        await builder.build([doc], { validation: true });

        const diags: PipelineDiagnostic[] = doc.diagnostics?.map((d: { severity?: number; message: string }) => ({
            severity: d.severity === 1 ? 'error' : 'warning',
            message:  d.message,
        })) ?? [];

        const parseErrors = diags.filter(d => d.severity === 'error');
        if (parseErrors.length > 0 || !doc.parseResult.value) {
            return { def: undefined, diagnostics: diags };
        }

        const def = mapPipeline(doc.parseResult.value as Parameters<typeof mapPipeline>[0]);
        const semanticDiags = validatePipeline(def);

        return { def, diagnostics: [...diags, ...semanticDiags] };
    } finally {
        // Always remove — keeps the cache clean even when a parse error
        // or thrown build leaves the document otherwise stranded.
        documents.deleteDocument(parsedUri);
    }
}
