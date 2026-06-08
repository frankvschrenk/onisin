// services.ts — creates the Langium language services for the pipeline grammar.
// Generated code in src/generated/ is produced by: bun run lang:generate

import type { Module } from 'langium';
import {
    inject,
    createDefaultCoreModule,
    createDefaultSharedCoreModule,
} from 'langium';
import type {
    LangiumSharedCoreServices,
    LangiumCoreServices,
    PartialLangiumCoreServices,
} from 'langium';
import { PipelineGeneratedModule, OnisinPipelineGeneratedSharedModule } from './generated/module.js';

export type OnisinPipelineAddedServices = Record<string, never>;

export type OnisinPipelineServices = LangiumCoreServices & OnisinPipelineAddedServices;

export const OnisinPipelineModule: Module<OnisinPipelineServices, PartialLangiumCoreServices> = {};

/**
 * createOnisinPipelineServices constructs all Langium services for the
 * .pipeline language and wires them together.
 */
export function createOnisinPipelineServices(context: Parameters<typeof createDefaultSharedCoreModule>[0]) {
    const shared = inject(
        createDefaultSharedCoreModule(context),
        OnisinPipelineGeneratedSharedModule,
    );
    const OnisinPipeline = inject(
        createDefaultCoreModule({ shared }),
        PipelineGeneratedModule,
        OnisinPipelineModule,
    );
    return { shared, OnisinPipeline };
}
