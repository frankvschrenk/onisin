// types.ts — Runtime-shaped view and domain definitions.
//
// These types are decoupled from Langium AST nodes: they carry no
// `$container`, no `$type` discriminator strings, no parser cruft.
// Tagged unions use plain `kind: "..."` so renderers can dispatch
// with a simple `switch (el.kind)`.
//
// Two top-level definitions are exported:
//
//   - `ViewDef`   — the renderable shape of a `view` block, with
//                   toolbar, body, and bound table sources.
//   - `DomainDef` — the data-shape of a `domain` block, with field
//                   metadata, permissions, relations, lookup sources
//                   and AI hints. The renderer in `renderer/llm-chunk`
//                   turns this into an LLM-friendly description for
//                   embedding into pgvector.
//
// The mapper in `mapper/view.ts` and `mapper/domain.ts` produces
// these from the corresponding Langium AST.

// ─── Shared primitives ───────────────────────────────────────────────

/** A "<domain>.<field>" reference resolved into its two parts. */
export interface FieldRefDef {
	domain: string;
	field: string;
}

/** A widget format hint such as `currency` or `number:0`. */
export interface FormatDef {
	kind: "currency" | "number" | "percent" | "datetime" | "date" | "time";
	/** Either a digit-count for "number:2" or a date style. */
	detail?: number | "short" | "medium" | "long" | "full";
}

/** Spacing value: token (xs/sm/md/lg/xl) or numeric scale. */
export type SpacingDef =
	| { kind: "token"; value: "xs" | "sm" | "md" | "lg" | "xl" }
	| { kind: "scale"; value: number };

/** Layout/spacing modifier shared by containers and widgets. */
export type LayoutModDef =
	| { kind: "padding"; prop: "p" | "pt" | "pb" | "pl" | "pr" | "px" | "py"; value: SpacingDef }
	| { kind: "margin"; prop: "m" | "mt" | "mb" | "ml" | "mr" | "mx" | "my"; value: SpacingDef }
	| { kind: "gap"; value: SpacingDef }
	| { kind: "cols"; value: number }
	| { kind: "expand" }
	| { kind: "scroll"; value: boolean };

/** Presentation target for navigation actions. */
export type PresentationTargetDef = "tab" | "modal" | "window" | "replace";

/** Navigation modifier on toolbar new / view actions. */
export type NavModifierDef =
	| { kind: "bind"; source: FieldRefDef }
	| { kind: "as"; target: PresentationTargetDef }
	| { kind: "confirm"; message: string };

// ─── Toolbar ─────────────────────────────────────────────────────────

export type ToolbarItemDef =
	| { kind: "save" }
	| { kind: "delete"; confirm?: string }
	| { kind: "exit" }
	| { kind: "refresh"; caption: string }
	| { kind: "new"; caption: string; target: string; navMods: NavModifierDef[] };

// ─── View-level actions on a Table (on_select etc.) ──────────────────

export type ViewActionEventDef = "on_select" | "on_new" | "on_edit";

export interface ViewActionDef {
	event: ViewActionEventDef;
	target: string;
	navMods: NavModifierDef[];
}

// ─── Body elements (containers + widgets + table) ────────────────────

export type BodyElementDef =
	| SectionDef
	| StackDef
	| RowDef
	| GridDef
	| TabsDef
	| AccordionDef
	| CardDef
	| TableDef
	| DividerDef
	| SeparatorDef
	| WidgetDef
	| ButtonDef
	| LinkDef
	| RichTextDef
	| IconDef;

// Layout containers ---------------------------------------------------

export interface SectionDef {
	kind: "section";
	caption?: string;
	mods: LayoutModDef[];
	body: BodyElementDef[];
}

export interface StackDef {
	kind: "stack";
	mods: LayoutModDef[];
	body: BodyElementDef[];
}

export interface RowDef {
	kind: "row";
	mods: LayoutModDef[];
	body: BodyElementDef[];
}

export interface GridDef {
	kind: "grid";
	cols: number;
	mods: LayoutModDef[];
	body: BodyElementDef[];
}

export interface TabsDef {
	kind: "tabs";
	mods: LayoutModDef[];
	tabs: TabPaneDef[];
}

export interface TabPaneDef {
	caption: string;
	body: BodyElementDef[];
}

export interface AccordionDef {
	kind: "accordion";
	mods: LayoutModDef[];
	items: AccordionItemDef[];
}

export interface AccordionItemDef {
	caption: string;
	open: boolean;
	body: BodyElementDef[];
}

export interface CardDef {
	kind: "card";
	caption?: string;
	mods: LayoutModDef[];
	body: BodyElementDef[];
}

export interface DividerDef {
	kind: "divider";
}

export interface SeparatorDef {
	kind: "separator";
}

// Tables --------------------------------------------------------------

export interface TableDef {
	kind: "table";
	rowSource: FieldRefDef;
	actions: ViewActionDef[];
	columns: ColumnDef[];
}

export interface ColumnDef {
	field: FieldRefDef;
	caption: string;
	width?: number;
	format?: FormatDef;
}

// Bindable widgets ----------------------------------------------------

/** Fixed list of widget kinds, mirroring `WidgetKind` in the grammar. */
export type WidgetKindDef =
	| "text"
	| "textarea"
	| "number"
	| "password"
	| "email"
	| "date"
	| "daterange"
	| "time"
	| "select"
	| "multiselect"
	| "combobox"
	| "radio"
	| "check"
	| "switch"
	| "slider"
	| "rangeslider"
	| "rating"
	| "color"
	| "file"
	| "progress"
	| "badge"
	| "avatar"
	| "tags"
	| "json";

export interface WidgetDef {
	kind: "widget";
	widget: WidgetKindDef;
	caption?: string;
	bind: FieldRefDef;
	/** Pre-bucketed modifiers; renderer-friendly access. */
	readOnly: boolean;
	focus: boolean;
	expand: boolean;
	placeholder?: string;
	format?: FormatDef;
	min?: number;
	max?: number;
	step?: number;
	layout: LayoutModDef[];
}

// Static widgets ------------------------------------------------------

export interface ButtonDef {
	kind: "button";
	caption: string;
	actionRef?: string;
	layout: LayoutModDef[];
}

export interface LinkDef {
	kind: "link";
	caption: string;
	href: string;
	layout: LayoutModDef[];
}

export interface IconDef {
	kind: "icon";
	name: string;
	size?: number;
	layout: LayoutModDef[];
}

export interface RichTextDef {
	kind: "richtext";
	spans: RichTextSpanDef[];
}

export interface RichTextSpanDef {
	style: "plain" | "bold" | "italic" | "heading" | "subheading" | "mono";
	text: string;
}

// ─── ViewDef (top-level) ─────────────────────────────────────────────

/**
 * One participating domain on a view, with its local alias.
 *
 * The alias is what bind paths quote inside widgets — `p.firstname`,
 * `a.city`. When the author omitted the parenthesised alias in the
 * source, the mapper sets `alias` equal to `name` so that bindings
 * keep working without the alias notation. The first binding in the
 * list is the *primary* domain: it is the target of the toolbar's
 * save and delete actions.
 */
export interface ViewDomainBindingDef {
	/** Domain name, e.g. `person`. */
	name: string;
	/** Local alias in this view, e.g. `p`; defaults to `name`. */
	alias: string;
	/** True for the first entry — the save/delete target. */
	primary: boolean;
}

/**
 * One auto-refresh subscription declared at the top of a view.
 *
 * Reads as `auto_refresh on <domain>.<event>` in the DSL. The view
 * runtime subscribes to the `<domain>:<event>` channel of the
 * frontend-internal pub/sub bus; whenever the channel fires, the
 * view re-fetches whatever query produced its data.
 *
 * Today only `event === "changed"` is meaningful — the detail tab
 * publishes that channel after a successful save or delete. The
 * shape is kept open-ended so future event names plug in without a
 * grammar change.
 */
export interface AutoRefreshDef {
	/** Domain that fires the event. Free-form name; not validated against the workspace yet. */
	domain: string;
	/** Event name, e.g. `"changed"`. */
	event: string;
}

export interface ViewDef {
	/** View identifier, e.g. `person_list`. */
	name: string;
	/** Human title, e.g. `"Personen"`. */
	title: string;
	/**
	 * Domains this view binds to via `over`, in declaration order.
	 * Always non-empty after a successful parse. The first entry is
	 * the primary (save/delete target); secondary entries contribute
	 * read-only context for joined / cross-domain renderings.
	 */
	domains: ViewDomainBindingDef[];
	/**
	 * True when this view is flagged as the natural starting point
	 * for its primary domain. The pre-LLM resolver picks the default
	 * view when the user mentions a domain without naming a specific
	 * one. At most one view per domain may carry the flag; uniqueness
	 * is enforced by the workspace-level validator.
	 */
	default: boolean;
	/**
	 * Optional `auto_refresh on <domain>.<event>` declaration. When
	 * set, list-view-style renderers should subscribe to the named
	 * domain event and re-fetch their data on every fire.
	 */
	autoRefresh?: AutoRefreshDef;
	toolbar: ToolbarItemDef[];
	body: BodyElementDef[];
}

// ─── DomainDef (full shape) ──────────────────────────────────────────
//
// Mirrors the grammar one-to-one but in plain runtime form. Field
// modifiers are pre-bucketed (readOnly/filterable/optionsRef) so the
// LLM-chunk renderer and any future consumer can read them directly
// without walking a heterogenous union.
//
// AI hints are kept as an ordered list rather than a map so authors
// retain control over presentation order in the rendered chunk.

/** Field type literal as accepted by the grammar. */
export type FieldTypeDef =
	| "int"
	| "float"
	| "string"
	| "text"
	| "bool"
	| "date"
	| "datetime";

/** Operator on a filter example. */
export type ExampleOpDef = "eq" | "ne" | "lt" | "le" | "gt" | "ge" | "like" | "in";

/**
 * One author-supplied filter example for an LLM. Carries the operator,
 * a literal value (string or number), and a free-form description.
 */
export interface ExampleDef {
	op: ExampleOpDef;
	/** Stringified value — numbers come through as their string form. */
	value: string;
	/** Whether the original literal was a string (for quoting hints). */
	valueIsString: boolean;
	/** Author note, e.g. "Vorname enthält 'Anna'". */
	description: string;
}

/** Permission verb. Stringly-typed because the grammar is. */
export type PermissionActionDef = "read" | "write" | "delete";

/** Role-scoped permission entry on a domain. */
export interface PermissionDef {
	role: string;
	actions: PermissionActionDef[];
}

/** Relation kind on a Relation member. */
export type RelationKindDef = "has_many" | "has_one" | "belongs_to";

/** A relation from this domain to another, with the binding columns. */
export interface RelationDef {
	name: string;
	kind: RelationKindDef;
	target: string;
	localField: string;
	foreignField: string;
}

/**
 * A lookup source for a dropdown field.
 *
 * `valueField` and `labelField` are the source columns; the LLM-side
 * GraphQL contract still refers to the *aliased* `value` and `label`
 * fields when querying a meta — see `renderer/llm-chunk` for the
 * canonical shape.
 */
export interface MetaDef {
	name: string;
	table: string;
	valueField: string;
	labelField: string;
	orderBy?: string;
	dsn?: string;
}

/**
 * One field on a domain. Modifiers are pre-bucketed for fast lookup;
 * `examples` is kept in author order so rendered chunks stay stable.
 */
export interface DomainFieldDef {
	name: string;
	type: FieldTypeDef;
	readOnly: boolean;
	filterable: boolean;
	/** Name of the Meta this field draws options from, if any. */
	optionsRef?: string;
	examples: ExampleDef[];
}

/** Free-form AI hint authored at the domain level. */
export interface AiHintDef {
	name: string;
	body: string;
}

/**
 * Top-level domain definition, the full runtime mirror of the grammar.
 *
 * `source` is the underlying table (e.g. `person`) and `dsn` is the
 * datasource registered in oosp config; both are needed to assemble
 * GraphQL queries in the renderer.
 */
export interface DomainDef {
	name: string;
	source: string;
	dsn: string;
	permissions: PermissionDef[];
	fields: DomainFieldDef[];
	relations: RelationDef[];
	metas: MetaDef[];
	aiHints: AiHintDef[];
	/**
	 * Author-supplied alternative names for the domain. Drawn from
	 * `aliases [...]` clauses in the source — multiple clauses are
	 * concatenated in declaration order. Empty for domains that do
	 * not declare any.
	 *
	 * The renderer in `renderer/aliases.ts` appends these to the
	 * heuristic singular/plural variants so the resolver in
	 * apps/oos can pick up irregular plurals, English borrowings
	 * and synonyms without going through the LLM.
	 */
	aliases: string[];
}
