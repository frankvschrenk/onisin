// dispatch.tsx — Single entry point that maps a BodyElementDef to the
// right Mantine renderer. Imported recursively by every container.

import type { ReactElement } from "react";
import type { BodyElementDef } from "oos-dsls-ts/types";

import {
	AccordionRenderer,
	CardRenderer,
	DividerRenderer,
	GridRenderer,
	RowRenderer,
	SectionRenderer,
	SeparatorRenderer,
	StackRenderer,
	TabsRenderer,
} from "./containers";
import {
	ButtonRenderer,
	IconRenderer,
	LinkRenderer,
	RichTextRenderer,
} from "./static";
import { TableRenderer } from "./table";
import { BoundWidget } from "./widgets";

/**
 * BodyElementRenderer dispatches a single BodyElementDef to its
 * specialised Mantine component. The exhaustive switch ensures every
 * element kind is handled — TypeScript will flag a new kind that
 * wasn't added here.
 */
export function BodyElementRenderer({
	def,
}: {
	def: BodyElementDef;
}): ReactElement {
	switch (def.kind) {
		case "section":
			return <SectionRenderer def={def} />;
		case "stack":
			return <StackRenderer def={def} />;
		case "row":
			return <RowRenderer def={def} />;
		case "grid":
			return <GridRenderer def={def} />;
		case "tabs":
			return <TabsRenderer def={def} />;
		case "accordion":
			return <AccordionRenderer def={def} />;
		case "card":
			return <CardRenderer def={def} />;
		case "table":
			return <TableRenderer def={def} />;
		case "divider":
			return <DividerRenderer />;
		case "separator":
			return <SeparatorRenderer />;
		case "widget":
			return <BoundWidget def={def} />;
		case "button":
			return <ButtonRenderer def={def} />;
		case "link":
			return <LinkRenderer def={def} />;
		case "icon":
			return <IconRenderer def={def} />;
		case "richtext":
			return <RichTextRenderer def={def} />;
	}
}
