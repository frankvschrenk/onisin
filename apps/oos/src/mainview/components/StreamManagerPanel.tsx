// StreamManagerPanel.tsx — manage public.event_streams per mapping.
//
// Layout: mapping list left, streams for active mapping right.
//
// Interaction:
//   - Left-click on a stream  → opens a stream_detail tab for event entry
//   - Right-click on a stream → context menu with "Editieren" + "Löschen"
//   - "Editieren" in context menu → also opens the stream_detail tab
//   - "Löschen" in context menu → deletes after confirmation
//   - "New Stream" button → modal to create a new stream

import { useCallback, useEffect, useState } from "react";
import {
	ActionIcon,
	Alert,
	Box,
	Button,
	Divider,
	Group,
	Loader,
	Menu,
	Modal,
	ScrollArea,
	Select,
	Stack,
	Text,
	TextInput,
	Title,
	UnstyledButton,
} from "@mantine/core";
import {
	IconDots,
	IconPencil,
	IconPlus,
	IconRefresh,
	IconTrash,
	IconX,
} from "@tabler/icons-react";

import { rpc } from "../rpc";
import { useAppSettings } from "../store/settings";
import { openStreamDetail } from "../store/tabs";
import type { EventMapping } from "../event-types";

// ─── Types ───────────────────────────────────────────────────────────

/**
 * StreamRow mirrors the shape returned by oos.cmd.event_streams.list.
 * Field names use snake_case to match the NATS-side payload directly —
 * the SQL projects `m.name AS mapping_name` etc., so an extra rename
 * step in oosai or here would only hide the convention.
 */
interface StreamRow {
	stream:            string;
	description:       string;
	event_mapping_id?: number | null;
	mapping_name?:     string | null;
	tag?:              string | null;
	eventCount?:       number;
}

// ─── Panel ────────────────────────────────────────────────────────────

export function StreamManagerPanel() {
	const { loaded } = useAppSettings();
	const [mappings,        setMappings]        = useState<EventMapping[]>([]);
	const [activeMapping,   setActiveMapping]   = useState<EventMapping | null>(null);
	const [mappingsLoading, setMappingsLoading] = useState(false);

	useEffect(() => {
		if (!loaded) return;
		setMappingsLoading(true);
		rpc.getEventMappings({})
			.then((res) => {
				if (res.error) return;
				const body = JSON.parse(res.json) as { mappings?: EventMapping[] };
				const list = (body.mappings ?? []).filter((m) => m.enabled);
				setMappings(list);
				if (list.length > 0 && activeMapping === null) {
					setActiveMapping(list[0]!);
				}
			})
			.finally(() => setMappingsLoading(false));
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [loaded]);

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
				<Text size="sm" fw={600} c="dimmed" px="sm" py="xs">Mappings</Text>
				<Divider />
				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p={4}>
						{mappingsLoading && <Loader size={14} m="sm" />}
						{mappings.map((m) => (
							<MappingRow
								key={m.name}
								name={m.name}
								active={activeMapping?.name === m.name}
								onClick={() => setActiveMapping(m)}
							/>
						))}
						{!mappingsLoading && mappings.length === 0 && (
							<Text size="xs" c="dimmed" p="sm">No mappings found.</Text>
						)}
					</Stack>
				</ScrollArea>
			</Box>

			{/* Right: streams */}
			<Box style={{ display: "flex", flexDirection: "column", minHeight: 0, overflow: "hidden" }}>
				{activeMapping ? (
					<StreamList
						mapping={activeMapping}
					/>
				) : (
					<Box p="lg">
						<Text c="dimmed" size="sm">Select a mapping to see its streams.</Text>
					</Box>
				)}
			</Box>
		</Box>
	);
}

// ─── MappingRow ───────────────────────────────────────────────────────

function MappingRow({ name, active, onClick }: {
	name:    string;
	active:  boolean;
	onClick: () => void;
}) {
	return (
		<UnstyledButton
			onClick={onClick}
			style={{
				width: "100%",
				padding: "6px 8px",
				borderRadius: 4,
				borderLeft: active
					? "3px solid var(--mantine-color-brand-6)"
					: "3px solid transparent",
				background: active ? "var(--mantine-color-brand-0)" : "transparent",
				transition: "background 80ms ease",
			}}
		>
			<Text size="sm" fw={active ? 600 : 500} c={active ? "indigo.7" : undefined}>
				{name}
			</Text>
		</UnstyledButton>
	);
}

// ─── StreamList ───────────────────────────────────────────────────────

function StreamList({ mapping }: {
	mapping:  EventMapping;
}) {
	const [streams,    setStreams]    = useState<StreamRow[]>([]);
	const [loading,    setLoading]   = useState(false);
	const [error,      setError]     = useState<string | null>(null);
	const [newOpen,    setNewOpen]   = useState(false);
	const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
	const [deleting,   setDeleting]  = useState(false);

	const load = useCallback(async () => {
		setLoading(true);
		setError(null);
		try {
			const res = await rpc.listAllStreams({});
			if (res.error) { setError(res.error); return; }
			const body = JSON.parse(res.json) as { streams?: StreamRow[] };
			const all = body.streams ?? [];
			setStreams(all.filter((s) => s.mapping_name === mapping.name));
		} catch (err) {
			setError(err instanceof Error ? err.message : String(err));
		} finally {
			setLoading(false);
		}
	}, [mapping.name]);

	useEffect(() => { void load(); }, [load]);

	async function handleDelete(stream: string) {
		setDeleting(true);
		setError(null);
		try {
			const res = await rpc.deleteEventStream({ stream });
			if (!res.ok) { setError(res.error ?? "delete failed"); return; }
			setDeleteTarget(null);
			await load();
		} finally {
			setDeleting(false);
		}
	}

	function handleEdit(stream: StreamRow) {
		openStreamDetail({
			mapping:     mapping.name,
			sourceTable: mapping.source_table,
			streamId:    stream.stream,
		});
	}

	return (
		<>
			<Group
				px="md" py="xs" justify="space-between"
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)" }}
			>
				<Title order={5}>{mapping.name}</Title>
				<Group gap="xs">
					<ActionIcon variant="subtle" onClick={load} disabled={loading} aria-label="Refresh">
						{loading ? <Loader size={12} /> : <IconRefresh size={15} />}
					</ActionIcon>
					<Button size="xs" leftSection={<IconPlus size={13} />} onClick={() => setNewOpen(true)}>
						New Stream
					</Button>
				</Group>
			</Group>

			<ScrollArea style={{ flex: 1 }}>
				<Box p="md">
					{error && (
						<Alert color="red" mb="sm" icon={<IconX size={14} />}
							onClose={() => setError(null)} withCloseButton>
							{error}
						</Alert>
					)}
					{!loading && streams.length === 0 && (
						<Text c="dimmed" size="sm">
							No streams found for <b>{mapping.name}</b>. Create one with "New Stream".
						</Text>
					)}
					<Stack gap={0}>
						{streams.map((s) => (
							<StreamRowItem
								key={s.stream}
								stream={s}
								onEdit={() => handleEdit(s)}
								onDelete={() => setDeleteTarget(s.stream)}
							/>
						))}
					</Stack>
				</Box>
			</ScrollArea>

			{/* New stream modal */}
			<NewStreamModal
				opened={newOpen}
				
				eventMappingId={mapping.id}
				mappingName={mapping.name}
				onClose={() => setNewOpen(false)}
				onCreated={() => { setNewOpen(false); void load(); }}
			/>

			{/* Delete confirmation modal */}
			<Modal
				opened={deleteTarget !== null}
				onClose={() => setDeleteTarget(null)}
				title="Stream löschen"
				size="sm"
			>
				<Stack gap="sm">
					<Text size="sm">
						Stream <strong>{deleteTarget}</strong> löschen? Alle zugehörigen Events werden ebenfalls gelöscht.
					</Text>
					{error && <Alert color="red" icon={<IconX size={14} />}>{error}</Alert>}
					<Group justify="flex-end">
						<Button variant="subtle" onClick={() => setDeleteTarget(null)} disabled={deleting}>
							Abbrechen
						</Button>
						<Button
							color="red"
							loading={deleting}
							onClick={() => deleteTarget && void handleDelete(deleteTarget)}
						>
							Löschen
						</Button>
					</Group>
				</Stack>
			</Modal>
		</>
	);
}

// ─── StreamRowItem ────────────────────────────────────────────────────

function StreamRowItem({ stream, onEdit, onDelete }: {
	stream:   StreamRow;
	onEdit:   () => void;
	onDelete: () => void;
}) {
	return (
		<Box
			py="xs" px="sm"
			style={{
				display: "flex",
				justifyContent: "space-between",
				alignItems: "center",
				borderBottom: "1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-5))",
				cursor: "pointer",
			}}
			onClick={onEdit}
		>
			<Box style={{ flex: 1, minWidth: 0 }}>
				<Text size="sm" fw={500} lineClamp={1}>{stream.stream}</Text>
				{stream.description && (
					<Text size="xs" c="dimmed" lineClamp={1}>{stream.description}</Text>
				)}
			</Box>

			{/* Context menu — stops propagation so row click doesn't fire */}
			<Menu shadow="md" width={160} position="bottom-end" withArrow>
				<Menu.Target>
					<ActionIcon
						variant="subtle"
						color="gray"
						size="sm"
						aria-label="Stream actions"
						onClick={(e) => e.stopPropagation()}
					>
						<IconDots size={15} />
					</ActionIcon>
				</Menu.Target>
				<Menu.Dropdown onClick={(e) => e.stopPropagation()}>
					<Menu.Item
						leftSection={<IconPencil size={14} />}
						onClick={onEdit}
					>
						Editieren
					</Menu.Item>
					<Menu.Item
						leftSection={<IconTrash size={14} />}
						color="red"
						onClick={onDelete}
					>
						Löschen
					</Menu.Item>
				</Menu.Dropdown>
			</Menu>
		</Box>
	);
}

// ─── NewStreamModal ───────────────────────────────────────────────────

function NewStreamModal({ opened, eventMappingId, mappingName, onClose, onCreated }: {
	opened:         boolean;
	eventMappingId: number | null;
	mappingName:    string;
	onClose:        () => void;
	onCreated:      () => void;
}) {
	const [stream,      setStream]      = useState("");
	const [description, setDescription] = useState("");
	const [tag,         setTag]         = useState<string | null>(null);
	const [tags,        setTags]        = useState<string[]>([]);
	const [saving,      setSaving]      = useState(false);
	const [error,       setError]       = useState<string | null>(null);

	// Load available tags for this mapping when the modal opens.
	useEffect(() => {
		if (!opened || !mappingName) return;
		rpc.getEventTags({ mapping: mappingName })
			.then((res) => {
				if (res.error) return;
				const body = JSON.parse(res.json) as { tags?: string[] };
				setTags(body.tags ?? []);
			})
			.catch(() => setTags([]));
	}, [opened, mappingName]);

	function reset() {
		setStream(""); setDescription(""); setTag(null); setError(null);
	}

	async function handleSubmit() {
		if (!stream.trim()) return;
		setSaving(true);
		setError(null);
		try {
			const res = await rpc.createEventStream({
				stream:         stream.trim(),
				description:    description.trim(),
				eventMappingId: eventMappingId,
				tag:            tag ?? undefined,
			});
			if (!res.ok) { setError(res.error ?? "create failed"); return; }
			reset();
			onCreated();
		} finally {
			setSaving(false);
		}
	}

	return (
		<Modal
			opened={opened}
			onClose={() => { reset(); onClose(); }}
			title="New Event Stream"
			size="sm"
		>
			<Stack gap="sm">
				<TextInput
					label="Stream ID"
					description="Unique identifier, e.g. fall-2024-0099"
					placeholder="fall-2024-0099"
					value={stream}
					onChange={(e) => setStream(e.currentTarget.value)}
					data-autofocus
					onKeyDown={(e) => { if (e.key === "Enter") void handleSubmit(); }}
				/>
				<TextInput
					label="Description"
					description="Human-readable label shown in the stream picker"
					placeholder="Short stream description"
					value={description}
					onChange={(e) => setDescription(e.currentTarget.value)}
					onKeyDown={(e) => { if (e.key === "Enter") void handleSubmit(); }}
				/>
				<Select
					label="Tag"
					description="Context tag — filters which event types are available in this stream"
					placeholder="Tag wählen (optional)..."
					data={tags}
					value={tag}
					onChange={(v) => setTag(v)}
					clearable
					comboboxProps={{ withinPortal: true }}
				/>
				{error && <Alert color="red" icon={<IconX size={14} />}>{error}</Alert>}
				<Group justify="flex-end" mt="xs">
					<Button variant="subtle" onClick={() => { reset(); onClose(); }}>Cancel</Button>
					<Button onClick={() => void handleSubmit()} loading={saving} disabled={!stream.trim()}>
						Create
					</Button>
				</Group>
			</Stack>
		</Modal>
	);
}
