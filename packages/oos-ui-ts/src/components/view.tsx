// view.tsx — Top-level OnisinView component.
//
// Renders a complete ViewDef: title, toolbar, and body. The host
// wires up:
//
//   - `state`     — a ViewState instance, kept across renders so the
//                   form stays alive between data loads.
//   - `onToolbar` — invoked when the user clicks Save / Delete / etc.
//                   The host decides what to do (POST /mutation, etc).
//   - `onAction`  — invoked when a TableDef row triggers on_select
//                   or similar; the host handles navigation.
//
// All three are optional for the editor preview path: the preview
// just shows the form populated with mock data and ignores actions.
//
// Internally the view publishes `onAction` via a small React Context
// (parallel to ViewStateContext) so renderers like TableRenderer can
// pick it up without prop-drilling through every container. Today
// only the table reads it; future widgets that fire actions (a
// link's on_click, a button) will go through the same context.

import {
	Button,
	Group,
	Stack,
	Title,
} from "@mantine/core";
import type { ReactElement } from "react";
import type {
	DomainDef,
	ToolbarItemDef,
	ViewActionDef,
	ViewDef,
} from "oos-dsls-ts/types";

import {
	ViewActionsContext,
	ViewDomainContext,
	ViewStateContext,
} from "../hooks";
import type { ViewState } from "../state";
import { BodyElementRenderer } from "./dispatch";

export interface OnisinViewProps {
	def: ViewDef;
	state: ViewState;
	/**
	 * Primary domain backing this view. Optional so preview / editor
	 * paths can render without it; widgets that need domain context
	 * (dropdowns reading their `optionsRef`) fall back to empty when
	 * absent.
	 */
	domain?: DomainDef;
	onToolbar?: (item: ToolbarItemDef) => void;
	onAction?: (action: ViewActionDef, row: Record<string, unknown>) => void;
}

export function OnisinView({
	def,
	state,
	domain,
	onToolbar,
	onAction,
}: OnisinViewProps): ReactElement {
	return (
		<ViewStateContext.Provider value={state}>
			<ViewDomainContext.Provider value={domain ?? null}>
			<ViewActionsContext.Provider value={{ onAction, onToolbar }}>
				<Stack gap="md">
					{def.toolbar.length > 0 && (
					<Group justify="flex-end" align="center">
						<Group gap="xs">
							{def.toolbar.map((item, i) => (
								<ToolbarButton
									key={i}
									item={item}
									onClick={() => onToolbar?.(item)}
								/>
							))}
						</Group>
					</Group>
					)}
					<Stack gap="md">
						{def.body.map((el, i) => (
							<BodyElementRenderer key={i} def={el} />
						))}
					</Stack>
				</Stack>
			</ViewActionsContext.Provider>
			</ViewDomainContext.Provider>
		</ViewStateContext.Provider>
	);
}

function ToolbarButton({
	item,
	onClick,
}: {
	item: ToolbarItemDef;
	onClick: () => void;
}): ReactElement {
	switch (item.kind) {
		case "save":
			return (
				<Button onClick={onClick} variant="filled">
					Save
				</Button>
			);
		case "delete":
			return (
				<Button onClick={onClick} color="red" variant="light">
					Delete
				</Button>
			);
		case "exit":
			return (
				<Button onClick={onClick} variant="subtle">
					Exit
				</Button>
			);
		case "refresh":
			return (
				<Button onClick={onClick} variant="default">
					{item.caption}
				</Button>
			);
		case "new":
			return (
				<Button onClick={onClick} variant="light">
					{item.caption}
				</Button>
			);
	}
}
