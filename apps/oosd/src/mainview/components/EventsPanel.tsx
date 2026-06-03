// EventsPanel.tsx — admin panel for creating new event contexts.
//
// Layout: two-column grid matching the Domain/View editor pane.
//   Left  — selectable list of existing event mappings from public.event_mappings.
//           Clicking a row shows its details on the right in read-only mode.
//   Right — "New" mode (default): creation form + generated SQL preview.
//           "Detail" mode (mapping selected): read-only mapping fields.
//
// Form design:
//   The user enters only a single name (e.g. "warehouse"). All table
//   names and identifiers are derived automatically:
//     source table   = {name}_events
//     embeddings     = {name}_embeddings
//     notify fn      = notify_{name}_events
//     notify channel = {name}_events_notify
//
//   Text field and ID field stay visible with sensible defaults ("text" / "id")
//   because they are content-relevant choices the user should confirm.
//
//   An "Advanced" toggle reveals the derived names so a user who needs
//   to point at a pre-existing table can override them.
//
// The SQL is generated client-side and executed on the bun side via
// Prisma's $executeRawUnsafe, split by the dollar-quote-aware splitSql().

import { useCallback, useEffect, useState } from "react";
import {
	Alert,
	Badge,
	Box,
	Button,
	Code,
	Divider,
	Group,
	Loader,
	ScrollArea,
	Stack,
	Text,
	TextInput,
	Title,
	UnstyledButton,
} from "@mantine/core";
import { IconChevronDown, IconChevronRight, IconCheck, IconRefresh, IconX } from "@tabler/icons-react";
import Editor from "@monaco-editor/react";

import { rpc } from "../rpc";
import type { EventMapping } from "../rpc";

// ─── Types ───────────────────────────────────────────────────────────

interface FormState {
	/** The only required user input. Drives all derived identifiers. */
	name:            string;
	/** Column that holds the text to embed. Default: "text". */
	textField:       string;
	/** Primary-key column name. Default: "id". */
	idField:         string;
	// Advanced overrides — empty means "use the derived value".
	sourceTable:     string;
	embeddingsTable: string;
}

const EMPTY_FORM: FormState = {
	name:            "",
	textField:       "text",
	idField:         "id",
	sourceTable:     "",
	embeddingsTable: "",
};

// ─── Derived values ───────────────────────────────────────────────────

/** All identifiers derived from the form, merging advanced overrides. */
interface Derived {
	sourceTable:     string;
	embeddingsTable: string;
	notifyFn:        string;
	notifyChannel:   string;
}

function derive(f: FormState): Derived {
	const base           = f.name.trim();
	const sourceTable    = f.sourceTable.trim()     || `${base}_events`;
	const embeddingsTable = f.embeddingsTable.trim() || `${base}_embeddings`;
	return {
		sourceTable,
		embeddingsTable,
		notifyFn:      `notify_${sourceTable}`,
		notifyChannel: `${sourceTable}_notify`,
	};
}

// ─── SQL generation ───────────────────────────────────────────────────

/**
 * buildSql produces the full DDL block for a new event context:
 *   1. CREATE OR REPLACE FUNCTION for the PL/pgSQL notify trigger
 *   2. CREATE TABLE public.<sourceTable> with trigger + FK
 *   3. CREATE TABLE public.<embeddingsTable> with ivfflat index
 *   4. INSERT INTO public.event_mappings
 */
function buildSql(f: FormState): string {
	const d = derive(f);
	return `-- Notify trigger function
CREATE OR REPLACE FUNCTION ${d.notifyFn}()
\tRETURNS trigger AS $$
BEGIN
\tPERFORM pg_notify('${d.notifyChannel}', row_to_json(NEW)::text);
\tRETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Source events table
CREATE TABLE public.${d.sourceTable} (
\tid serial4 NOT NULL,
\tstream varchar(200) NOT NULL,
\tevent_type varchar(200) NOT NULL,
\t${f.textField} text NOT NULL,
\tpayload jsonb DEFAULT '{}'::jsonb NOT NULL,
\tprocessed bool DEFAULT false NOT NULL,
\tcreated_at timestamptz DEFAULT now() NOT NULL,
\tCONSTRAINT ${d.sourceTable}_pkey PRIMARY KEY (id)
);

CREATE TRIGGER ${d.sourceTable}_notify
\tAFTER INSERT ON public.${d.sourceTable}
\tFOR EACH ROW EXECUTE FUNCTION ${d.notifyFn}();

ALTER TABLE public.${d.sourceTable}
\tADD CONSTRAINT ${d.sourceTable}_stream_fkey
\tFOREIGN KEY (stream) REFERENCES public.event_streams(stream);

-- Embeddings table
CREATE TABLE public.${d.embeddingsTable} (
\tid uuid DEFAULT gen_random_uuid() NOT NULL,
\tsource_id text NOT NULL,
\tstream_id text NOT NULL,
\tevent_type text NOT NULL,
\ttext_content text NOT NULL,
\tmetadata jsonb DEFAULT '{}'::jsonb NOT NULL,
\tembedding public.vector(384) NULL,
\tcreated_at timestamptz DEFAULT now() NOT NULL,
\tCONSTRAINT ${d.embeddingsTable}_pkey PRIMARY KEY (id),
\tCONSTRAINT ${d.embeddingsTable}_source_id_key UNIQUE (source_id)
);

CREATE INDEX ${d.embeddingsTable}_stream_idx
\tON public.${d.embeddingsTable} USING btree (stream_id, created_at);

CREATE INDEX ${d.embeddingsTable}_vector_idx
\tON public.${d.embeddingsTable} USING ivfflat (embedding vector_cosine_ops)
\tWITH (lists='100');

-- Event mapping registration
INSERT INTO public.event_mappings (
\tname, source_schema, source_table,
\tsource_text_field, source_id_field, notify_channel,
\ttarget_schema, target_table, enabled
) VALUES (
\t'${f.name.trim()}', 'public', '${d.sourceTable}',
\t'${f.textField}', '${f.idField}', '${d.notifyChannel}',
\t'public', '${d.embeddingsTable}', true
);
`;
}

/** Returns true when the form has enough data to generate valid SQL. */
function isFormValid(f: FormState): boolean {
	return f.name.trim() !== "" && f.textField.trim() !== "" && f.idField.trim() !== "";
}

// ─── Component ───────────────────────────────────────────────────────

export function EventsPanel({ disabled }: { disabled: boolean }) {
	const [mappings,  setMappings]  = useState<EventMapping[]>([]);
	const [loading,   setLoading]   = useState(false);
	const [selected,  setSelected]  = useState<EventMapping | null>(null);
	const [form,      setForm]      = useState<FormState>(EMPTY_FORM);
	const [advanced,  setAdvanced]  = useState(false);
	const [executing, setExecuting] = useState(false);
	const [result,    setResult]    = useState<{ ok: boolean; msg: string } | null>(null);

	const sql = selected === null && isFormValid(form) ? buildSql(form) : "";

	// ─── Load mappings ────────────────────────────────────────────

	const loadMappings = useCallback(async () => {
		// listEventMappings goes through NATS — no Prisma needed.
		setLoading(true);
		try {
			const res = await rpc.listEventMappings({});
			setMappings(res.mappings);
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => { void loadMappings(); }, [loadMappings]);

	// ─── Form helpers ─────────────────────────────────────────────

	function setField<K extends keyof FormState>(key: K, value: string) {
		setForm((prev) => ({ ...prev, [key]: value }));
		setResult(null);
	}

	function startNew() {
		setSelected(null);
		setForm(EMPTY_FORM);
		setAdvanced(false);
		setResult(null);
	}

	// ─── Execute ──────────────────────────────────────────────────

	async function handleExecute() {
		if (!isFormValid(form) || executing) return;
		setExecuting(true);
		setResult(null);
		try {
			const res = await rpc.execEventContext({ sql });
			if (res.ok) {
				setResult({ ok: true, msg: "Event context created successfully." });
				setForm(EMPTY_FORM);
				setAdvanced(false);
				await loadMappings();
			} else {
				setResult({ ok: false, msg: res.error ?? "unknown error" });
			}
		} finally {
			setExecuting(false);
		}
	}

	// ─── Render ───────────────────────────────────────────────────

	return (
		<Box
			style={{
				display: "grid",
				gridTemplateColumns: "280px 1fr",
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
					<Text size="sm" fw={600} c="dimmed">Event Mappings</Text>
					<Button
						variant="subtle"
						size="compact-xs"
						leftSection={<IconRefresh size={12} />}
						onClick={loadMappings}
						disabled={disabled || loading}
					>
						{loading ? <Loader size={10} /> : "Refresh"}
					</Button>
				</Group>
				<Divider />
				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p={4}>
						<MappingRow
							name="New context"
							subtitle="Create a new event context"
							active={selected === null}
							onClick={startNew}
						/>
						<Divider my={4} />
						{mappings.length === 0 && !loading && (
							<Text size="xs" c="dimmed" p="sm">No mappings found.</Text>
						)}
						{mappings.map((m) => (
							<MappingRow
								key={m.id}
								name={m.name}
								subtitle={`${m.source_table} → ${m.target_table}`}
								badge={m.enabled ? "ON" : "OFF"}
								badgeColor={m.enabled ? "green" : "gray"}
								active={selected?.id === m.id}
								onClick={() => { setSelected(m); setResult(null); }}
							/>
						))}
					</Stack>
				</ScrollArea>
			</Box>

			{/* Right: detail or new form */}
			<Box style={{ display: "flex", flexDirection: "column", minHeight: 0, overflow: "hidden" }}>
				{selected !== null ? (
					<MappingDetail mapping={selected} />
				) : (
					<NewContextForm
						form={form}
						sql={sql}
						advanced={advanced}
						disabled={disabled}
						executing={executing}
						result={result}
						onField={setField}
						onToggleAdvanced={() => setAdvanced((v) => !v)}
						onExecute={handleExecute}
						onDismissResult={() => setResult(null)}
					/>
				)}
			</Box>
		</Box>
	);
}

// ─── MappingRow ───────────────────────────────────────────────────────

interface MappingRowProps {
	name:       string;
	subtitle:   string;
	badge?:     string;
	badgeColor?: string;
	active:     boolean;
	onClick:    () => void;
}

function MappingRow({ name, subtitle, badge, badgeColor, active, onClick }: MappingRowProps) {
	return (
		<UnstyledButton
			onClick={onClick}
			style={{
				width: "100%",
				padding: "6px 8px",
				borderRadius: 4,
				borderLeft: active
					? "3px solid var(--mantine-color-indigo-6)"
					: "3px solid transparent",
				background: active ? "var(--mantine-color-indigo-0)" : "transparent",
				transition: "background 80ms ease",
			}}
		>
			<Group justify="space-between" wrap="nowrap">
				<Text size="sm" fw={active ? 600 : 500} lineClamp={1} c={active ? "indigo.7" : undefined}>
					{name}
				</Text>
				{badge && (
					<Badge size="xs" variant="light" color={badgeColor ?? "gray"}>{badge}</Badge>
				)}
			</Group>
			<Text size="xs" c="dimmed" lineClamp={1}>{subtitle}</Text>
		</UnstyledButton>
	);
}

// ─── MappingDetail ────────────────────────────────────────────────────

function MappingDetail({ mapping }: { mapping: EventMapping }) {
	const rows: Array<[string, string]> = [
		["Name",           mapping.name],
		["Source schema",  mapping.source_schema],
		["Source table",   mapping.source_table],
		["Text field",     mapping.source_text_field],
		["ID field",       mapping.source_id_field],
		["Notify channel", mapping.notify_channel],
		["Target schema",  mapping.target_schema],
		["Target table",   mapping.target_table],
		["Enabled",        mapping.enabled ? "yes" : "no"],
		["Created",        mapping.created_at],
	];
	return (
		<Box style={{ flex: 1, overflow: "auto" }} p="md">
			<Group mb="md" align="center" gap="sm">
				<Title order={5}>{mapping.name}</Title>
				<Badge color={mapping.enabled ? "green" : "gray"} variant="light">
					{mapping.enabled ? "Enabled" : "Disabled"}
				</Badge>
			</Group>
			<Stack gap="xs">
				{rows.map(([label, value]) => (
					<Group key={label} gap="md" wrap="nowrap" align="flex-start">
						<Text size="sm" fw={500} w={160} style={{ flexShrink: 0 }} c="dimmed">{label}</Text>
						<Code fz="xs">{value}</Code>
					</Group>
				))}
			</Stack>
		</Box>
	);
}

// ─── NewContextForm ───────────────────────────────────────────────────

interface NewContextFormProps {
	form:            FormState;
	sql:             string;
	advanced:        boolean;
	disabled:        boolean;
	executing:       boolean;
	result:          { ok: boolean; msg: string } | null;
	onField:         <K extends keyof FormState>(key: K, value: string) => void;
	onToggleAdvanced: () => void;
	onExecute:       () => void;
	onDismissResult: () => void;
}

function NewContextForm({
	form, sql, advanced, disabled, executing, result,
	onField, onToggleAdvanced, onExecute, onDismissResult,
}: NewContextFormProps) {
	// Show derived values in the Advanced section as placeholders.
	const d = derive(form);

	return (
		<>
			<Box px="md" py="sm" style={{ borderBottom: "1px solid var(--mantine-color-default-border)" }}>
				<Title order={5} mb="sm">New Event Context</Title>
				<Stack gap="sm">

					{/* ── Primary input ── */}
					<TextInput
						label="Name"
						description="e.g. warehouse — all identifiers are derived from this"
						placeholder="warehouse"
						value={form.name}
						onChange={(e) => onField("name", e.currentTarget.value)}
						disabled={disabled}
						size="xs"
					/>

					<Group grow>
						<TextInput
							label="Text field"
							description="Column to embed"
							placeholder="text"
							value={form.textField}
							onChange={(e) => onField("textField", e.currentTarget.value)}
							disabled={disabled}
							size="xs"
						/>
						<TextInput
							label="ID field"
							description="Primary key column"
							placeholder="id"
							value={form.idField}
							onChange={(e) => onField("idField", e.currentTarget.value)}
							disabled={disabled}
							size="xs"
						/>
					</Group>

					{/* ── Advanced toggle ── */}
					<UnstyledButton onClick={onToggleAdvanced}>
						<Group gap={4}>
							{advanced
								? <IconChevronDown size={13} color="var(--mantine-color-dimmed)" />
								: <IconChevronRight size={13} color="var(--mantine-color-dimmed)" />
							}
							<Text size="xs" c="dimmed">Advanced — override derived names</Text>
						</Group>
					</UnstyledButton>

					{advanced && (
						<Stack gap="xs">
							<TextInput
								label="Source table"
								description={`Default: ${d.sourceTable || "{name}_events"}`}
								placeholder={d.sourceTable || "{name}_events"}
								value={form.sourceTable}
								onChange={(e) => onField("sourceTable", e.currentTarget.value)}
								disabled={disabled}
								size="xs"
							/>
							<TextInput
								label="Embeddings table"
								description={`Default: ${d.embeddingsTable || "{name}_embeddings"}`}
								placeholder={d.embeddingsTable || "{name}_embeddings"}
								value={form.embeddingsTable}
								onChange={(e) => onField("embeddingsTable", e.currentTarget.value)}
								disabled={disabled}
								size="xs"
							/>
						</Stack>
					)}

				</Stack>
			</Box>

			{/* SQL preview */}
			<Box style={{ flex: 1, minHeight: 0, overflow: "hidden" }}>
				<Group
					px="md" py="xs" justify="space-between"
					style={{ borderBottom: "1px solid var(--mantine-color-default-border)" }}
				>
					<Text size="sm" fw={500} c="dimmed">Generated SQL (read-only)</Text>
					<Button
						size="xs"
						onClick={onExecute}
						disabled={disabled || !isFormValid(form) || executing}
						loading={executing}
						color="green"
					>
						Execute
					</Button>
				</Group>

				{result && (
					<Alert
						mx="md" mt="xs"
						color={result.ok ? "green" : "red"}
						icon={result.ok ? <IconCheck size={14} /> : <IconX size={14} />}
						onClose={onDismissResult}
						withCloseButton
					>
						{result.msg}
					</Alert>
				)}

				<Box style={{ height: "calc(100% - 40px)", overflow: "hidden" }}>
					<Editor
						height="100%"
						language="sql"
						value={sql || "-- Enter a name above to preview the generated SQL."}
						options={{
							readOnly:             true,
							minimap:              { enabled: false },
							fontSize:             12,
							wordWrap:             "on",
							scrollBeyondLastLine: false,
							automaticLayout:      true,
							lineNumbers:          "on",
						}}
					/>
				</Box>
			</Box>
		</>
	);
}
