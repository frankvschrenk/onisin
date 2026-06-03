// MappingTypesPanel.tsx — assigns event types to mappings.
//
// Layout: two columns.
//   Left  — list of event mappings (police, support, warehouse…).
//   Right — all grammar types as a checklist. Checked = assigned
//           to this mapping. Toggling saves immediately via
//           setMappingEventTypes (overwrites the jsonb array).
//
// The event_types column on event_mappings is a plain jsonb string
// array — no join table, no FK. This panel is the only place where
// it is edited.

import { useCallback, useEffect, useState } from "react";
import {
	Box,
	Button,
	Checkbox,
	Divider,
	Group,
	Loader,
	ScrollArea,
	Stack,
	Text,
} from "@mantine/core";
import { IconRefresh } from "@tabler/icons-react";

import { rpc } from "../rpc";
import type { EventMapping, GrammarType } from "../rpc";

// ─── Component ───────────────────────────────────────────────────────

export function MappingTypesPanel({ disabled }: { disabled: boolean }) {
	// ── Left: mappings ──
	const [mappings,        setMappings]        = useState<EventMapping[]>([]);
	const [mappingsLoading, setMappingsLoading] = useState(false);
	const [selectedMapping, setSelectedMapping] = useState<EventMapping | null>(null);

	// ── Right: all grammar types + which are checked ──
	const [allTypes,     setAllTypes]     = useState<GrammarType[]>([]);
	const [typesLoading, setTypesLoading] = useState(false);
	const [checked,      setChecked]      = useState<Set<string>>(new Set());
	const [saving,       setSaving]       = useState(false);

	// ─── Load mappings ────────────────────────────────────────────

	const loadMappings = useCallback(async () => {

		setMappingsLoading(true);
		try {
			const res = await rpc.listEventMappings({});
			setMappings(res.mappings);
		} finally {
			setMappingsLoading(false);
		}
	}, [disabled]);

	useEffect(() => { void loadMappings(); }, [loadMappings]);

	// ─── Load all grammar types ───────────────────────────────────

	const loadAllTypes = useCallback(async () => {

		setTypesLoading(true);
		try {
			const res = await rpc.listGrammarTypes({});
			setAllTypes(res.types);
		} finally {
			setTypesLoading(false);
		}
	}, [disabled]);

	useEffect(() => { void loadAllTypes(); }, [loadAllTypes]);

	// ─── Select mapping → load its event_types ───────────────────

	async function selectMapping(m: EventMapping) {
		setSelectedMapping(m);
		setSaving(false);
		const res = await rpc.getMappingEventTypes({ mappingId: m.id });
		setChecked(new Set(res.eventTypes ?? []));
	}

	// ─── Toggle a type ────────────────────────────────────────────

	async function handleToggle(name: string) {
		if (!selectedMapping || saving) return;
		const next = new Set(checked);
		if (next.has(name)) next.delete(name);
		else                next.add(name);
		setChecked(next);
		// Save immediately — no "Save" button needed, it's a simple toggle.
		setSaving(true);
		try {
			await rpc.setMappingEventTypes({
				mappingId:  selectedMapping.id,
				eventTypes: Array.from(next),
			});
		} finally {
			setSaving(false);
		}
	}

	// ─── Render ───────────────────────────────────────────────────

	return (
		<Box
			style={{
				display: "grid",
				gridTemplateColumns: "220px 1fr",
				height: "100%",
				minHeight: 0,
			}}
		>
			{/* Left: mapping list */}
			<Box
				style={{
					borderRight: "1px solid var(--mantine-color-default-border)",
					display: "flex",
					flexDirection: "column",
					minHeight: 0,
				}}
			>
				<Group px="sm" py="xs" justify="space-between">
					<Text size="sm" fw={600} c="dimmed">Mappings</Text>
					<Button
						variant="subtle" size="compact-xs"
						leftSection={<IconRefresh size={12} />}
						onClick={loadMappings}
						disabled={disabled || mappingsLoading}
					>
						{mappingsLoading ? <Loader size={10} /> : "Refresh"}
					</Button>
				</Group>
				<Divider />
				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p={4}>
						{mappings.map((m) => (
							<MappingRow
								key={m.id}
								mapping={m}
								active={selectedMapping?.id === m.id}
								onClick={() => void selectMapping(m)}
							/>
						))}
					</Stack>
				</ScrollArea>
			</Box>

			{/* Right: checklist of all grammar types */}
			<Box
				style={{
					display: "flex",
					flexDirection: "column",
					minHeight: 0,
				}}
			>
				<Group px="md" py="xs" justify="space-between">
					<Text size="sm" fw={600} c="dimmed">
						{selectedMapping
							? `Event types for ${selectedMapping.name}`
							: "Select a mapping"}
					</Text>
					{saving && <Loader size={12} />}
					{typesLoading && <Loader size={12} />}
				</Group>
				<Divider />
				<ScrollArea style={{ flex: 1 }}>
					{selectedMapping === null ? (
						<Text size="xs" c="dimmed" p="md">Select a mapping to assign event types.</Text>
					) : (
						<Stack gap="xs" p="md">
							{allTypes.map((t) => (
								<Checkbox
									key={t.id}
									label={t.name}
									checked={checked.has(t.name)}
									onChange={() => void handleToggle(t.name)}
									disabled={saving}
								/>
							))}
							{allTypes.length === 0 && !typesLoading && (
								<Text size="xs" c="dimmed">
									No event types in the grammar library yet. Add them in Event Types first.
								</Text>
							)}
						</Stack>
					)}
				</ScrollArea>
			</Box>
		</Box>
	);
}

// ─── MappingRow ───────────────────────────────────────────────────────

function MappingRow({
	mapping,
	active,
	onClick,
}: {
	mapping: EventMapping;
	active:  boolean;
	onClick: () => void;
}) {
	return (
		<Box
			onClick={onClick}
			style={{
				padding: "6px 8px",
				borderRadius: 4,
				borderLeft: active
					? "3px solid var(--mantine-color-indigo-6)"
					: "3px solid transparent",
				background: active ? "var(--mantine-color-indigo-0)" : "transparent",
				cursor: "pointer",
				transition: "background 80ms ease",
			}}
		>
			<Text size="sm" fw={active ? 600 : 500} c={active ? "indigo.7" : undefined}>
				{mapping.name}
			</Text>
			<Text size="xs" c="dimmed">{mapping.source_table}</Text>
		</Box>
	);
}
