// KvStorePanel.tsx — Generic JetStream KV browser for oosd.
//
// Layout: two columns.
//   Left  — bucket list with New / Delete bucket actions.
//   Right — key list for the selected bucket + JSON editor for the
//            selected entry. New key dialog, Delete key with confirm.
//
// Values are stored and displayed as JSON. The editor does not
// validate the JSON before saving — invalid JSON will be stored as-is
// and shown as a parse error on the next load.

import { useCallback, useEffect, useState } from "react";
import {
	Alert,
	Box,
	Button,
	Code,
	Divider,
	Group,
	Loader,
	Modal,
	ScrollArea,
	Stack,
	Text,
	TextInput,
	Title,
	UnstyledButton,
} from "@mantine/core";
import {
	IconDatabase,
	IconPlus,
	IconRefresh,
	IconTrash,
	IconX,
} from "@tabler/icons-react";
import Editor, { type OnMount } from "@monaco-editor/react";
import * as monacoType from "monaco-editor";
import { rpc } from "../rpc";
import { installMonacoClipboard } from "../clipboard";
import type { KvBucketInfo, KvEntry } from "../rpc";

// ─── Component ───────────────────────────────────────────────────────

export function KvStorePanel({ disabled }: { disabled: boolean }) {
	// ── Bucket list ──
	const [buckets,        setBuckets]        = useState<KvBucketInfo[]>([]);
	const [bucketsLoading, setBucketsLoading] = useState(false);
	const [selectedBucket, setSelectedBucket] = useState<string | null>(null);
	const [bucketError,    setBucketError]    = useState<string | null>(null);

	// ── New bucket dialog ──
	const [newBucketOpen,  setNewBucketOpen]  = useState(false);
	const [newBucketName,  setNewBucketName]  = useState("");
	const [newBucketBusy,  setNewBucketBusy]  = useState(false);

	// ── Key list ──
	const [entries,        setEntries]        = useState<KvEntry[]>([]);
	const [entriesLoading, setEntriesLoading] = useState(false);
	const [selectedKey,    setSelectedKey]    = useState<string | null>(null);
	const [entryError,     setEntryError]     = useState<string | null>(null);

	// ── Entry editor ──
	const [editorValue,    setEditorValue]    = useState("");
	const [saveBusy,       setSaveBusy]       = useState(false);
	const [saveOk,         setSaveOk]         = useState(false);

	// ── New key dialog ──
	const [newKeyOpen,     setNewKeyOpen]     = useState(false);
	const [newKeyName,     setNewKeyName]     = useState("");
	const [newKeyBusy,     setNewKeyBusy]     = useState(false);

	// ── Delete confirms ──
	const [deleteBucketTarget, setDeleteBucketTarget] = useState<string | null>(null);
	const [deleteKeyTarget,    setDeleteKeyTarget]    = useState<string | null>(null);

	const loadBuckets = useCallback(async () => {
		setBucketsLoading(true);
		setBucketError(null);
		try {
			const res = await rpc.listKvBuckets({});
			if (res.error) setBucketError(res.error);
			else setBuckets(res.buckets);
		} finally {
			setBucketsLoading(false);
		}
	}, []);

	const loadKeys = useCallback(async (bucket: string) => {
		setEntriesLoading(true);
		setEntryError(null);
		setSelectedKey(null);
		setEditorValue("");
		try {
			const res = await rpc.listKvKeys({ bucket });
			if (res.error) setEntryError(res.error);
			else setEntries(res.entries);
		} finally {
			setEntriesLoading(false);
		}
	}, []);

	useEffect(() => { void loadBuckets(); }, [loadBuckets]);

	function selectBucket(name: string) {
		setSelectedBucket(name);
		void loadKeys(name);
	}

	function selectKey(entry: KvEntry) {
		setSelectedKey(entry.key);
		setEditorValue(JSON.stringify(entry.value, null, 2));
		setSaveOk(false);
	}

	async function onCreateBucket() {
		if (!newBucketName.trim()) return;
		setNewBucketBusy(true);
		try {
			const res = await rpc.createKvBucket({ bucket: newBucketName.trim() });
			if (res.error) { setBucketError(res.error); return; }
			setNewBucketOpen(false);
			setNewBucketName("");
			await loadBuckets();
		} finally {
			setNewBucketBusy(false);
		}
	}

	async function onDeleteBucket(name: string) {
		try {
			const res = await rpc.deleteKvBucket({ bucket: name });
			if (res.error) { setBucketError(res.error); return; }
			if (selectedBucket === name) { setSelectedBucket(null); setEntries([]); }
			await loadBuckets();
		} finally {
			setDeleteBucketTarget(null);
		}
	}

	async function onCreateKey() {
		if (!newKeyName.trim() || !selectedBucket) return;
		setNewKeyBusy(true);
		try {
			const res = await rpc.putKvEntry({ bucket: selectedBucket, key: newKeyName.trim(), value: [] });
			if (res.error) { setEntryError(res.error); return; }
			setNewKeyOpen(false);
			setNewKeyName("");
			await loadKeys(selectedBucket);
		} finally {
			setNewKeyBusy(false);
		}
	}

	async function onSaveEntry() {
		if (!selectedBucket || !selectedKey) return;
		setSaveBusy(true);
		try {
			let parsed: unknown;
			try { parsed = JSON.parse(editorValue); }
			catch { parsed = editorValue; } // store raw string if JSON parse fails
			const res = await rpc.putKvEntry({ bucket: selectedBucket, key: selectedKey, value: parsed });
			if (res.error) { setEntryError(res.error); return; }
			setSaveOk(true);
			await loadKeys(selectedBucket);
		} finally {
			setSaveBusy(false);
		}
	}

	async function onDeleteKey(key: string) {
		if (!selectedBucket) return;
		try {
			const res = await rpc.deleteKvEntry({ bucket: selectedBucket, key });
			if (res.error) { setEntryError(res.error); return; }
			if (selectedKey === key) { setSelectedKey(null); setEditorValue(""); }
			await loadKeys(selectedBucket);
		} finally {
			setDeleteKeyTarget(null);
		}
	}

	return (
		<Box style={{ display: "flex", height: "100%", overflow: "hidden" }}>

			{/* ── Left: bucket list ── */}
			<Box style={{ width: 220, borderRight: "1px solid var(--mantine-color-default-border)", display: "flex", flexDirection: "column" }}>
				<Group p="xs" justify="space-between">
					<Text fw={600} size="sm">Buckets</Text>
					<Group gap={4}>
						<Button size="compact-xs" variant="subtle" leftSection={<IconRefresh size={12} />}
							loading={bucketsLoading} onClick={() => void loadBuckets()} disabled={disabled}>
							Refresh
						</Button>
						<Button size="compact-xs" variant="subtle" leftSection={<IconPlus size={12} />}
							onClick={() => setNewBucketOpen(true)} disabled={disabled}>
							New
						</Button>
					</Group>
				</Group>
				<Divider />
				{bucketError && <Alert color="red" p="xs" m="xs">{bucketError}</Alert>}
				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p="xs">
						{buckets.length === 0 && !bucketsLoading && (
							<Text size="xs" c="dimmed">No buckets yet.</Text>
						)}
						{buckets.map((b) => (
							<UnstyledButton key={b.name}
								onClick={() => selectBucket(b.name)}
								style={{
									padding: "6px 8px",
									borderRadius: 4,
									background: selectedBucket === b.name ? "var(--mantine-color-blue-light)" : undefined,
								}}>
								<Group justify="space-between" wrap="nowrap">
									<Group gap={6} wrap="nowrap">
										<IconDatabase size={14} />
										<Text size="xs" fw={selectedBucket === b.name ? 600 : 400} truncate>{b.name}</Text>
									</Group>
									<Group gap={4} wrap="nowrap">
										<Text size="xs" c="dimmed">{b.keys}</Text>
										<UnstyledButton onClick={(e) => { e.stopPropagation(); setDeleteBucketTarget(b.name); }}
											style={{ color: "var(--mantine-color-red-6)", display: "flex" }}>
											<IconTrash size={12} />
										</UnstyledButton>
									</Group>
								</Group>
							</UnstyledButton>
						))}
					</Stack>
				</ScrollArea>
			</Box>

			{/* ── Middle: key list ── */}
			<Box style={{ width: 240, borderRight: "1px solid var(--mantine-color-default-border)", display: "flex", flexDirection: "column" }}>
				{selectedBucket ? (
					<>
						<Group p="xs" justify="space-between">
							<Text fw={600} size="sm" truncate style={{ maxWidth: 120 }}>{selectedBucket}</Text>
							<Group gap={4}>
								<Button size="compact-xs" variant="subtle" leftSection={<IconRefresh size={12} />}
									loading={entriesLoading} onClick={() => void loadKeys(selectedBucket)}>
									Refresh
								</Button>
								<Button size="compact-xs" variant="subtle" leftSection={<IconPlus size={12} />}
									onClick={() => setNewKeyOpen(true)}>
									New
								</Button>
							</Group>
						</Group>
						<Divider />
						{entryError && <Alert color="red" p="xs" m="xs">{entryError}</Alert>}
						<ScrollArea style={{ flex: 1 }}>
							<Stack gap={0} p="xs">
								{entries.length === 0 && !entriesLoading && (
									<Text size="xs" c="dimmed">No keys yet.</Text>
								)}
								{entries.map((e) => (
									<UnstyledButton key={e.key}
										onClick={() => selectKey(e)}
										style={{
											padding: "6px 8px",
											borderRadius: 4,
											background: selectedKey === e.key ? "var(--mantine-color-blue-light)" : undefined,
										}}>
										<Group justify="space-between" wrap="nowrap">
											<Text size="xs" fw={selectedKey === e.key ? 600 : 400} truncate style={{ maxWidth: 160 }}>{e.key}</Text>
											<UnstyledButton onClick={(ev) => { ev.stopPropagation(); setDeleteKeyTarget(e.key); }}
												style={{ color: "var(--mantine-color-red-6)", display: "flex" }}>
												<IconTrash size={12} />
											</UnstyledButton>
										</Group>
									</UnstyledButton>
								))}
							</Stack>
						</ScrollArea>
					</>
				) : (
					<Box p="md"><Text size="xs" c="dimmed">Select a bucket.</Text></Box>
				)}
			</Box>

			{/* ── Right: JSON editor ── */}
			<Box style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
				{selectedKey ? (
					<>
						<Group p="xs" justify="space-between">
							<Stack gap={0}>
								<Text fw={600} size="sm">{selectedKey}</Text>
								<Text size="xs" c="dimmed">{selectedBucket}</Text>
							</Stack>
							<Group gap={6}>
								{saveOk && <IconX size={14} color="var(--mantine-color-green-6)" />}
								<Button size="compact-sm" loading={saveBusy} onClick={() => void onSaveEntry()}>
									Save
								</Button>
							</Group>
						</Group>
						<Divider />
						<Box style={{ flex: 1, overflow: "hidden" }}>
							<Editor
								height="100%"
								language="json"
								value={editorValue}
								onChange={(v) => { setEditorValue(v ?? ""); setSaveOk(false); }}
								options={{ minimap: { enabled: false }, fontSize: 13, tabSize: 2 }}
								onMount={(editor) => installMonacoClipboard(editor, monacoType)}
							/>
						</Box>
					</>
				) : (
					<Box p="md"><Text size="xs" c="dimmed">Select a key to edit its value.</Text></Box>
				)}
			</Box>

			{/* ── New bucket dialog ── */}
			<Modal opened={newBucketOpen} onClose={() => setNewBucketOpen(false)} title="New bucket" size="sm">
				<Stack>
					<TextInput
						label="Bucket name"
						placeholder="e.g. onisin-completion"
						value={newBucketName}
						onChange={(e) => setNewBucketName(e.currentTarget.value)}
						onKeyDown={(e) => { if (e.key === "Enter") void onCreateBucket(); }}
					/>
					<Button loading={newBucketBusy} onClick={() => void onCreateBucket()} disabled={!newBucketName.trim()}>
						Create
					</Button>
				</Stack>
			</Modal>

			{/* ── New key dialog ── */}
			<Modal opened={newKeyOpen} onClose={() => setNewKeyOpen(false)} title="New key" size="sm">
				<Stack>
					<TextInput
						label="Key"
						placeholder="e.g. onisin-pipeline.llm"
						value={newKeyName}
						onChange={(e) => setNewKeyName(e.currentTarget.value)}
						onKeyDown={(e) => { if (e.key === "Enter") void onCreateKey(); }}
					/>
					<Text size="xs" c="dimmed">Initial value: empty array <Code>[]</Code>. Edit after creation.</Text>
					<Button loading={newKeyBusy} onClick={() => void onCreateKey()} disabled={!newKeyName.trim()}>
						Create
					</Button>
				</Stack>
			</Modal>

			{/* ── Delete bucket confirm ── */}
			<Modal opened={!!deleteBucketTarget} onClose={() => setDeleteBucketTarget(null)} title="Delete bucket" size="sm">
				<Stack>
					<Text size="sm">Delete bucket <Code>{deleteBucketTarget}</Code> and all its keys? This cannot be undone.</Text>
					<Group justify="flex-end">
						<Button variant="default" onClick={() => setDeleteBucketTarget(null)}>Cancel</Button>
						<Button color="red" onClick={() => deleteBucketTarget && void onDeleteBucket(deleteBucketTarget)}>Delete</Button>
					</Group>
				</Stack>
			</Modal>

			{/* ── Delete key confirm ── */}
			<Modal opened={!!deleteKeyTarget} onClose={() => setDeleteKeyTarget(null)} title="Delete key" size="sm">
				<Stack>
					<Text size="sm">Delete key <Code>{deleteKeyTarget}</Code>?</Text>
					<Group justify="flex-end">
						<Button variant="default" onClick={() => setDeleteKeyTarget(null)}>Cancel</Button>
						<Button color="red" onClick={() => deleteKeyTarget && void onDeleteKey(deleteKeyTarget)}>Delete</Button>
					</Group>
				</Stack>
			</Modal>

		</Box>
	);
}
