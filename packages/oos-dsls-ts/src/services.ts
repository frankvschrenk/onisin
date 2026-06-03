// services.ts — Langium service factory for the onisin DSL languages.
//
// Builds and configures the Langium core services for both `domain`
// and `view` languages, using the shared service registry so the
// AstReflection is registered exactly once. Both languages can then
// be parsed by URI extension (`.domain` / `.view`) through the
// returned DocumentBuilder.
//
// This module is environment-agnostic: it uses `EmptyFileSystem` and
// works in browsers, web workers, Bun, and Node. Callers that need
// Node-specific filesystem access should swap the file-system
// provider before calling `inject`.
//
// Usage:
//
//   const services = createOnisinServices();
//   await services.parse({ language: "view", uri: "inmemory://x", text });
//
// The ServiceRegistry is exposed via `services.shared` for advanced
// use cases such as worker-hosted validators.

import {
	createDefaultCoreModule,
	createDefaultSharedCoreModule,
	EmptyFileSystem,
	inject,
	URI,
	type LangiumCoreServices,
	type LangiumSharedCoreServices,
	type LangiumDocument,
	type Module,
} from "langium";
import type { Diagnostic } from "vscode-languageserver-types";

import {
	DomainGeneratedModule,
	EventSchemaGeneratedModule,
	OnisinGeneratedSharedModule,
	ViewGeneratedModule,
} from "./generated/module";
import type { DomainModel, EventSchemaModel, ViewModel } from "./generated/ast";
import { DomainValidator, domainChecks } from "./validation/domain";
import { ViewValidator,   viewChecks   } from "./validation/view";

/**
 * Validator-injection module for the domain language. Adds a
 * `DomainValidator` factory to the validation slice of the core
 * services so the registry below has somewhere to bind to.
 */
const DomainValidatorModule: Module<
	LangiumCoreServices,
	{ validation: { DomainValidator: DomainValidator } }
> = {
	validation: {
		DomainValidator: () => new DomainValidator(),
	},
};

/**
 * Validator-injection module for the view language. Mirrors
 * DomainValidatorModule above; both modules add a single factory
 * to the validation slice so the registry can bind to it.
 */
const ViewValidatorModule: Module<
	LangiumCoreServices,
	{ validation: { ViewValidator: ViewValidator } }
> = {
	validation: {
		ViewValidator: () => new ViewValidator(),
	},
};

/** Languages supported by the onisin DSL package. */
export type OnisinLanguage = "domain" | "view" | "event-schema";

/** Mapping from language id to its top-level AST node type. */
export type OnisinRoot = {
	domain:         DomainModel;
	view:           ViewModel;
	"event-schema": EventSchemaModel;
};

/**
 * Bundle of Langium services initialised for both onisin DSLs and a
 * convenience `parse()` method that runs the full build pipeline.
 */
export interface OnisinServices {
	/** Shared services (DocumentBuilder, ServiceRegistry, ...). */
	shared: LangiumSharedCoreServices;
	/** Per-language services for `.domain`. */
	domain: LangiumCoreServices;
	/** Per-language services for `.view`. */
	view: LangiumCoreServices;
	/** Per-language services for `.event-schema`. */
	eventSchema: LangiumCoreServices;
	/**
	 * Parse and validate a single source buffer.
	 *
	 * Returns the parsed document together with its diagnostics. The
	 * caller decides what to do on errors — `parseStrict()` throws.
	 */
	parse<L extends OnisinLanguage>(req: ParseRequest<L>): Promise<ParseResult<L>>;
}

/** Input to `OnisinServices.parse()`. */
export interface ParseRequest<L extends OnisinLanguage> {
	language: L;
	/** URI used to register the document; extension is auto-fixed. */
	uri: string;
	/** Source text to parse. */
	text: string;
}

/** Output of `OnisinServices.parse()`. */
export interface ParseResult<L extends OnisinLanguage> {
	/** Top-level AST node, or undefined if parsing aborted entirely. */
	root: OnisinRoot[L] | undefined;
	/** Validation and parse diagnostics in LSP shape. */
	diagnostics: Diagnostic[];
	/** The full Langium document (for advanced use). */
	document: LangiumDocument<OnisinRoot[L]>;
}

/**
 * createOnisinServices instantiates both DSL services with shared
 * reflection and registers them with the ServiceRegistry so the
 * DocumentBuilder can dispatch by extension.
 */
export function createOnisinServices(): OnisinServices {
	const shared = inject(
		createDefaultSharedCoreModule(EmptyFileSystem),
		OnisinGeneratedSharedModule,
	);

	const domain = inject(
		createDefaultCoreModule({ shared }),
		DomainGeneratedModule,
		DomainValidatorModule,
	) as LangiumCoreServices & {
		validation: LangiumCoreServices["validation"] & {
			DomainValidator: DomainValidator;
		};
	};

	const view = inject(
		createDefaultCoreModule({ shared }),
		ViewGeneratedModule,
		ViewValidatorModule,
	) as LangiumCoreServices & {
		validation: LangiumCoreServices["validation"] & {
			ViewValidator: ViewValidator;
		};
	};

	const eventSchema = inject(
		createDefaultCoreModule({ shared }),
		EventSchemaGeneratedModule,
	);

	shared.ServiceRegistry.register(domain);
	shared.ServiceRegistry.register(view);
	shared.ServiceRegistry.register(eventSchema);

	// Wire the custom domain and view checks into their respective
	// validation registries. Bound after registry construction
	// because the registry is the service that actually dispatches
	// checks during build().
	domain.validation.ValidationRegistry.register(
		domainChecks(domain.validation.DomainValidator),
		domain.validation.DomainValidator,
	);
	view.validation.ValidationRegistry.register(
		viewChecks(view.validation.ViewValidator),
		view.validation.ViewValidator,
	);

	async function parse<L extends OnisinLanguage>(
		req: ParseRequest<L>,
	): Promise<ParseResult<L>> {
		const uri = languageUri(req.uri, req.language);
		const factory = shared.workspace.LangiumDocumentFactory;
		const documents = shared.workspace.LangiumDocuments;
		const builder = shared.workspace.DocumentBuilder;

		// Drop any earlier document at the same URI so the factory
		// does not throw on duplicates.
		if (documents.hasDocument(uri)) {
			await builder.update([], [uri]);
		}

		const document = factory.fromString<OnisinRoot[L]>(req.text, uri);
		documents.addDocument(document);
		await builder.build([document], { validation: true });

		return {
			root: document.parseResult.value,
			diagnostics: document.diagnostics ?? [],
			document,
		};
	}

	return { shared, domain, view, eventSchema, parse };
}

/**
 * languageUri rewrites the incoming model URI so its path ends in
 * `.domain` or `.view`. Langium's ServiceRegistry dispatches strictly
 * on file extension, so an extensionless URI like `inmemory://model/1`
 * fails with "no services for the extension ''". Callers keep their
 * original URI on their side; this function is internal.
 */
function languageUri(rawUri: string, language: OnisinLanguage): URI {
	const uri = URI.parse(rawUri);
	const ext = language === "domain"
		? ".domain"
		: language === "event-schema"
			? ".event-schema"
			: ".view";
	if (uri.path.endsWith(ext)) return uri;
	return uri.with({ path: `${uri.path}${ext}` });
}
