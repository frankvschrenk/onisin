// mapper/view-elements.ts — Maps body elements (containers, widgets,
// table, button, link, icon, richtext) from Langium AST to runtime
// BodyElementDef.
//
// The dispatcher `mapBodyElement` is the single entry point used
// recursively by container mappers. Unknown node types are swallowed
// (returns null) so a future grammar extension doesn't break older
// renderers.

import type {
	AccordionItem,
	BodyElement,
	Button,
	Card,
	Column,
	IconWidget,
	LinkWidget,
	NavModifier,
	RichText,
	Table,
	Tab,
	ToolbarItem,
	ViewAction,
	Widget,
	WidgetExpand,
	WidgetFocus,
	WidgetFormat,
	WidgetMax,
	WidgetMin,
	WidgetModifier,
	WidgetPlaceholder,
	WidgetReadOnly,
	WidgetStep,
} from "../generated/ast";
import type {
	AccordionDef,
	AccordionItemDef,
	BodyElementDef,
	ButtonDef,
	CardDef,
	ColumnDef,
	GridDef,
	IconDef,
	LinkDef,
	NavModifierDef,
	RichTextDef,
	RowDef,
	SectionDef,
	StackDef,
	TableDef,
	TabPaneDef,
	TabsDef,
	ToolbarItemDef,
	ViewActionDef,
	WidgetDef,
} from "../types";
import { mapFieldRef, mapFormat, mapLayoutMods } from "./common";

// ─── Toolbar ─────────────────────────────────────────────────────────

/** Map a toolbar item to its runtime form. */
export function mapToolbarItem(item: ToolbarItem): ToolbarItemDef | null {
	switch (item.$type) {
		case "ToolbarSave":
			return { kind: "save" };
		case "ToolbarDelete":
			return { kind: "delete", confirm: item.confirm };
		case "ToolbarExit":
			return { kind: "exit" };
		case "ToolbarRefresh":
			// Caption is optional in the grammar; the runtime always
			// surfaces a string so the renderer can drop it directly
			// onto the button without nullish-coalescing.
			return { kind: "refresh", caption: item.caption ?? "Aktualisieren" };
		case "ToolbarNew":
			return {
				kind: "new",
				caption: item.caption,
				target: item.target,
				navMods: mapNavModifiers(item.navMods),
			};
		default:
			return null;
	}
}

// ─── Navigation modifiers (used by toolbar new and view actions) ─────

export function mapNavModifiers(mods: NavModifier[] | undefined): NavModifierDef[] {
	if (!mods) return [];
	const out: NavModifierDef[] = [];
	for (const m of mods) {
		switch (m.$type) {
			case "Bind":
				out.push({ kind: "bind", source: mapFieldRef(m.source) });
				break;
			case "As":
				out.push({ kind: "as", target: m.target });
				break;
			case "Confirm":
				out.push({ kind: "confirm", message: m.message });
				break;
		}
	}
	return out;
}

// ─── Table + columns ─────────────────────────────────────────────────

export function mapTable(node: Table): TableDef {
	return {
		kind: "table",
		rowSource: mapFieldRef(node.rowSource),
		actions: node.actions.map(mapViewAction),
		columns: node.columns.map(mapColumn),
	};
}

export function mapViewAction(node: ViewAction): ViewActionDef {
	return {
		event: node.event,
		target: node.target,
		navMods: mapNavModifiers(node.navMods),
	};
}

export function mapColumn(node: Column): ColumnDef {
	let width: number | undefined;
	let format: ColumnDef["format"];

	for (const mod of node.mods) {
		switch (mod.$type) {
			case "ColumnWidth":
				width = mod.value;
				break;
			case "ColumnFormat":
				format = mapFormat(mod.fmt);
				break;
		}
	}

	return {
		field: mapFieldRef(node.field),
		caption: node.caption,
		width,
		format,
	};
}

// ─── Bindable widgets ────────────────────────────────────────────────
//
// Pre-bucket all modifiers into named fields on the runtime def. The
// AST keeps modifiers as a heterogenous array; callers prefer flat
// access (`def.readOnly`) over scanning the array each render.

export function mapWidget(node: Widget): WidgetDef {
	const def: WidgetDef = {
		kind: "widget",
		widget: node.kind,
		caption: node.caption,
		bind: mapFieldRef(node.bind),
		readOnly: false,
		focus: false,
		expand: false,
		layout: [],
	};

	const layoutMods: WidgetModifier[] = [];

	for (const mod of node.mods) {
		switch (mod.$type) {
			case "WidgetReadOnly":
				def.readOnly = true;
				break;
			case "WidgetFocus":
				def.focus = true;
				break;
			case "WidgetExpand":
				def.expand = true;
				break;
			case "WidgetPlaceholder":
				def.placeholder = (mod as WidgetPlaceholder).value;
				break;
			case "WidgetFormat":
				def.format = mapFormat((mod as WidgetFormat).fmt);
				break;
			case "WidgetMin":
				def.min = (mod as WidgetMin).value;
				break;
			case "WidgetMax":
				def.max = (mod as WidgetMax).value;
				break;
			case "WidgetStep":
				def.step = (mod as WidgetStep).value;
				break;
			default:
				// Layout modifiers (Padding, Margin, Gap, Cols, Expand,
				// ScrollFlag) come through here. The widget Expand is a
				// distinct node from LayoutMod's Expand: WidgetExpand has
				// $type "WidgetExpand", LayoutMod's Expand has $type
				// "Expand". Keep them separate.
				layoutMods.push(mod);
		}
	}

	def.layout = mapLayoutMods(layoutMods as Parameters<typeof mapLayoutMods>[0]);
	return def;
}

// ─── Static widgets ──────────────────────────────────────────────────

export function mapButton(node: Button): ButtonDef {
	return {
		kind: "button",
		caption: node.caption,
		actionRef: node.actionRef,
		layout: mapLayoutMods(node.mods),
	};
}

export function mapLink(node: LinkWidget): LinkDef {
	return {
		kind: "link",
		caption: node.caption,
		href: node.href,
		layout: mapLayoutMods(node.mods),
	};
}

export function mapIcon(node: IconWidget): IconDef {
	let size: number | undefined;
	const layoutMods: typeof node.mods = [];

	for (const mod of node.mods) {
		if (mod.$type === "IconSize") {
			size = mod.value;
		} else {
			layoutMods.push(mod);
		}
	}

	return {
		kind: "icon",
		name: node.name,
		size,
		layout: mapLayoutMods(layoutMods as Parameters<typeof mapLayoutMods>[0]),
	};
}

export function mapRichText(node: RichText): RichTextDef {
	return {
		kind: "richtext",
		spans: node.spans.map((s) => ({ style: s.style, text: s.text })),
	};
}

// ─── Body element dispatcher ─────────────────────────────────────────

/**
 * Single entry point used recursively by container mappers. Returns
 * null for unknown node types so future grammar additions degrade
 * gracefully.
 */
export function mapBodyElement(node: BodyElement): BodyElementDef | null {
	switch (node.$type) {
		case "Section":
			return {
				kind: "section",
				caption: node.caption,
				mods: mapLayoutMods(node.mods),
				body: mapBodyElements(node.body),
			} satisfies SectionDef;
		case "Stack":
			return {
				kind: "stack",
				mods: mapLayoutMods(node.mods),
				body: mapBodyElements(node.body),
			} satisfies StackDef;
		case "Row":
			return {
				kind: "row",
				mods: mapLayoutMods(node.mods),
				body: mapBodyElements(node.body),
			} satisfies RowDef;
		case "Grid":
			return {
				kind: "grid",
				cols: node.cols,
				mods: mapLayoutMods(node.mods),
				body: mapBodyElements(node.body),
			} satisfies GridDef;
		case "Tabs":
			return {
				kind: "tabs",
				mods: mapLayoutMods(node.mods),
				tabs: node.tabs.map(mapTabPane),
			} satisfies TabsDef;
		case "Accordion":
			return {
				kind: "accordion",
				mods: mapLayoutMods(node.mods),
				items: node.items.map(mapAccordionItem),
			} satisfies AccordionDef;
		case "Card":
			return {
				kind: "card",
				caption: node.caption,
				mods: mapLayoutMods(node.mods),
				body: mapBodyElements(node.body),
			} satisfies CardDef;
		case "Table":
			return mapTable(node);
		case "Divider":
			return { kind: "divider" };
		case "Separator":
			return { kind: "separator" };
		case "Widget":
			return mapWidget(node);
		case "Button":
			return mapButton(node);
		case "LinkWidget":
			return mapLink(node);
		case "IconWidget":
			return mapIcon(node);
		case "RichText":
			return mapRichText(node);
		default:
			return null;
	}
}

/** Helper: dispatch and filter unknowns. */
export function mapBodyElements(nodes: BodyElement[] | undefined): BodyElementDef[] {
	if (!nodes) return [];
	const out: BodyElementDef[] = [];
	for (const n of nodes) {
		const def = mapBodyElement(n);
		if (def) out.push(def);
	}
	return out;
}

function mapTabPane(node: Tab): TabPaneDef {
	return {
		caption: node.caption,
		body: mapBodyElements(node.body),
	};
}

function mapAccordionItem(node: AccordionItem): AccordionItemDef {
	return {
		caption: node.caption,
		open: node.open,
		body: mapBodyElements(node.body),
	};
}
