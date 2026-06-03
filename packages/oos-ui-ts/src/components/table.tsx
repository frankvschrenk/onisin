// table.tsx — Mantine renderer for `TableDef`.
//
// Rows come from the bind path `<rowSource.domain>.<rowSource.field>`,
// stored on the ViewState as a JSON-encoded array string (see
// `flattenInto` in envelope.ts — arrays land that way to keep the
// flat string store uniform). On render we parse once and memoise.
//
// Cell values are looked up by the column's field name on each row;
// columns get rendered through `formatValue` if the def has a format.
//
// `onAction` is read from the ambient ViewActionsContext rather
// than passed in as a prop — the dispatch tree only forwards `def`,
// and prop-drilling actions through every container would scale
// badly. The host (apps/oos's view / detail renderers) wraps the
// view in a ViewActionsContext provider; a preview without one
// silently renders the table as read-only.

import { ScrollArea, Table } from "@mantine/core";
import { useMemo, type ReactElement } from "react";
import type { ColumnDef, TableDef } from "oos-dsls-ts/types";

import { useBoundValue, useViewActions } from "../hooks";
import { formatValue } from "../format";

export interface TableRendererProps {
	def: TableDef;
}

export function TableRenderer({ def }: TableRendererProps): ReactElement {
	const rowsPath = `${def.rowSource.domain}.${def.rowSource.field}`;
	const [raw] = useBoundValue(rowsPath);
	const { onAction } = useViewActions();

	const rows = useMemo<Record<string, unknown>[]>(() => {
		if (!raw) return [];
		try {
			const parsed = JSON.parse(raw);
			return Array.isArray(parsed) ? parsed : [];
		} catch {
			return [];
		}
	}, [raw]);

	const onSelect = def.actions.find((a) => a.event === "on_select");
	const interactive = Boolean(onSelect && onAction);

	return (
		<ScrollArea>
			<Table striped highlightOnHover withTableBorder withColumnBorders>
				<Table.Thead>
					<Table.Tr>
						{def.columns.map((col) => (
							<Table.Th
								key={col.field.field}
								style={col.width ? { width: col.width } : undefined}
							>
								{col.caption}
							</Table.Th>
						))}
					</Table.Tr>
				</Table.Thead>
				<Table.Tbody>
					{rows.map((row, i) => (
						<Table.Tr
							key={i}
							onClick={
								interactive
									? () => onAction!(onSelect!, row)
									: undefined
							}
							style={interactive ? { cursor: "pointer" } : undefined}
						>
							{def.columns.map((col) => (
								<Table.Td key={col.field.field}>{cellValue(col, row)}</Table.Td>
							))}
						</Table.Tr>
					))}
				</Table.Tbody>
			</Table>
		</ScrollArea>
	);
}

function cellValue(col: ColumnDef, row: Record<string, unknown>): string {
	const v = row[col.field.field];
	if (v === undefined || v === null) return "";
	const raw = typeof v === "string" ? v : String(v);
	return formatValue(raw, col.format);
}
