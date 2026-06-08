// SettingsFinetuningPanel.tsx — Per-model fine-tuning settings.
//
// One persisted row per model id. The panel reads the row that
// matches the currently selected chat model (from app settings)
// and exposes the four levers shown in the legacy assistant:
//
//   - Temperature   (slider, 0..2)
//   - Timeout       (text "30s" / "5m" / "10m")
//   - Max tokens    (number)
//   - Top hits      (number — RAG chunks per oos_schema_search)
//
// At the top a Preset selector seeds those four levers from one
// of the named starting points in store/finetuning.ts (cloud,
// local). The selector never persists on its own — it just
// rewrites the local form state, the user still has to click Save
// for the values to land in IndexedDB. That keeps "I changed my
// mind" cheap (close the panel, the values revert).
//
// Each parameter has a small help-icon next to its label that
// explains in one sentence what raising / lowering it does. The
// `description` prop on Mantine's inputs gives a one-liner under
// the field; the tooltip carries the deeper "why" so the panel
// stays compact for users who already know what they want.
//
// "Save" persists; "Reset to defaults" deletes the row so a future
// load falls back to DEFAULT_FINETUNING. Switching the chat model
// in the Connection panel is reflected here automatically because
// the hook subscribes to the app-settings bus.

import { useEffect, useState, type ReactNode } from "react";
import {
	ActionIcon,
	Alert,
	Box,
	Button,
	Group,
	NumberInput,
	Paper,
	Select,
	Slider,
	Stack,
	Text,
	TextInput,
	Title,
	Tooltip,
} from "@mantine/core";
import { IconAlertCircle, IconHelpCircle } from "@tabler/icons-react";

import { useAppSettings } from "../store/settings";
import {
	DEFAULT_FINETUNING,
	FINETUNING_PRESETS,
	useFinetuning,
	type FinetuningPreset,
	type FinetuningSettings,
} from "../store/finetuning";

export function SettingsFinetuningPanel() {
	const { settings: app, loaded: appLoaded } = useAppSettings();
	const modelId = app.llmModel;

	if (appLoaded && !modelId) {
		return (
			<Paper p="lg" radius={0} style={{ height: "100%" }}>
				<Stack gap="md" maw={640}>
					<Title order={3}>Finetuning</Title>
					<Alert
						color="yellow"
						icon={<IconAlertCircle size={16} />}
						variant="light"
					>
						<Text size="sm">
							Wähle zuerst im Tab <b>Connection</b> ein Chat-Modell
							aus. Finetuning-Werte werden pro Modell gespeichert.
						</Text>
					</Alert>
				</Stack>
			</Paper>
		);
	}

	return <FinetuningEditor modelId={modelId} />;
}

interface FinetuningEditorProps {
	modelId: string;
}

/**
 * Display labels for the preset selector. Kept here (not in the
 * store) because they're a UI concern; the store knows the values,
 * the panel knows how to talk about them.
 */
const PRESET_LABELS: Record<FinetuningPreset, string> = {
	cloud: "Cloud (OpenAI, Anthropic, …)",
	local: "Lokal (Ollama, vLLM auf der Workstation)",
};

/**
 * One-line summary of what each preset is for. Shown in the
 * description under the Select so the user can see why a preset
 * exists without opening the docs.
 */
const PRESET_DESCRIPTIONS: Record<FinetuningPreset, string> = {
	cloud:
		"Lange Timeouts, große Antworten — Cloud-LLMs sind schnell und liefern lange Tokenströme zuverlässig.",
	local:
		"Kürzere Timeouts und Antworten — lokale Modelle sollen schnell scheitern statt fünf Minuten zu blockieren.",
};

function FinetuningEditor({ modelId }: FinetuningEditorProps) {
	const { settings, loaded, save } = useFinetuning(modelId);

	const [temperature, setTemperature] = useState(settings.temperature);
	const [timeoutText, setTimeoutText] = useState(formatTimeout(settings.timeoutMs));
	const [maxTokens,   setMaxTokens]   = useState<number>(settings.maxTokens);
	const [topHits,     setTopHits]     = useState<number>(settings.topHits);

	const [timeoutErr, setTimeoutErr] = useState<string | null>(null);
	const [savedAt,    setSavedAt]    = useState<string | null>(null);

	// Re-seed local state when the persisted row arrives or the
	// selected model changes.
	useEffect(() => {
		if (!loaded) return;
		setTemperature(settings.temperature);
		setTimeoutText(formatTimeout(settings.timeoutMs));
		setMaxTokens(settings.maxTokens);
		setTopHits(settings.topHits);
		setTimeoutErr(null);
		setSavedAt(null);
	}, [loaded, settings]);

	const applyPreset = (name: FinetuningPreset) => {
		const p = FINETUNING_PRESETS[name];
		setTemperature(p.temperature);
		setTimeoutText(formatTimeout(p.timeoutMs));
		setMaxTokens(p.maxTokens);
		setTopHits(p.topHits);
		setTimeoutErr(null);
		// Don't auto-save — user still has to click Save. Reset
		// the savedAt indicator so it's clear the form is dirty.
		setSavedAt(null);
	};

	const submit = async () => {
		const ms = parseTimeout(timeoutText);
		if (ms === null) {
			setTimeoutErr("Format: 30s, 5m, 10m, 1h");
			return;
		}
		setTimeoutErr(null);

		const next: FinetuningSettings = {
			temperature,
			timeoutMs: ms,
			maxTokens,
			topHits,
		};
		await save(next);
		setSavedAt(new Date().toLocaleTimeString());
	};

	const resetDefaults = async () => {
		await save({ ...DEFAULT_FINETUNING });
		setSavedAt(new Date().toLocaleTimeString());
	};

	return (
		<Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
			<Stack gap="lg" maw={640}>
				<div>
					<Title order={3}>Finetuning</Title>
					<Text size="sm" c="dimmed" mt={4}>
						Werte für <b>{modelId}</b>. Jedes Modell hat einen
						eigenen Satz; wechseln über{" "}
						<i>Connection → Chat model</i>.
					</Text>
				</div>

				<Select
					label={
						<LabelWithHelp
							text="Preset"
							help="Setzt Temperature, Timeout, Max Tokens und Top Hits auf einen Startpunkt für Cloud- oder Lokal-LLMs. Du kannst danach jeden Wert einzeln tweaken. Speichern erst mit Save."
						/>
					}
					placeholder="Wähle einen Startpunkt…"
					data={Object.entries(PRESET_LABELS).map(([value, label]) => ({
						value,
						label,
					}))}
					onChange={(v) => {
						if (v === "cloud" || v === "local") applyPreset(v);
					}}
					description={describePreset(temperature, timeoutText, maxTokens, topHits)}
					clearable={false}
					value={null /* never sticky — Preset is a one-shot apply */}
				/>

				<Stack gap="xs">
					<Group justify="space-between">
						<LabelWithHelp
							text="Temperature"
							help="Wie kreativ die LLM antwortet. 0 = deterministisch (gleiche Antwort bei gleichem Input), 1 = ausgewogen, >1 = experimentell. Für RAG-Workflows sollte das niedrig bleiben — die LLM soll Fakten aus dem Kontext nehmen, nicht halluzinieren."
							strong
						/>
						<Text size="sm" c="dimmed">
							{temperature.toFixed(2)}
						</Text>
					</Group>
					<Slider
						value={temperature}
						onChange={setTemperature}
						min={0}
						max={2}
						step={0.05}
						marks={[
							{ value: 0,   label: "0"   },
							{ value: 0.2, label: "0.2" },
							{ value: 1,   label: "1"   },
							{ value: 2,   label: "2"   },
						]}
					/>
					<Text size="xs" c="dimmed" mt={4}>
						Niedriger = sachlicher, höher = kreativer. RAG-Workflows
						laufen meist am besten bei 0.2.
					</Text>
				</Stack>

				<TextInput
					label={
						<LabelWithHelp
							text="Timeout"
							help="Wie lange der Agent auf eine Antwort wartet, bevor er abbricht. Für Cloud-LLMs großzügig setzen (5–10 Minuten — bei langen Antworten oder Lastspitzen). Für lokale Modelle eher kurz (60–90 Sekunden), damit ein hängender Generator den ganzen Loop nicht blockiert."
						/>
					}
					value={timeoutText}
					onChange={(e) => setTimeoutText(e.currentTarget.value)}
					placeholder="5m"
					error={timeoutErr ?? undefined}
					description="Wie lange auf die LLM gewartet wird. Format: 30s, 5m, 10m, 1h."
				/>

				<NumberInput
					label={
						<LabelWithHelp
							text="Max tokens"
							help="Obergrenze für die Länge der LLM-Antwort. 4096 ist ein guter Standard für Cloud-Modelle. Bei lokalen 7–14B-Modellen besser 1024–2048: kleinere Modelle driften auf langen Generationen häufig in unzusammenhängenden Text ab."
						/>
					}
					value={maxTokens}
					onChange={(v) => setMaxTokens(typeof v === "number" ? v : Number(v) || 0)}
					min={64}
					max={32_000}
					step={256}
					description="Maximale Antwortlänge. 4096 reicht für die meisten Fragen."
				/>

				<NumberInput
					label={
						<LabelWithHelp
							text="Top hits"
							help="Wie viele Schema-Chunks oos_schema_search der LLM pro Tool-Aufruf zurückgibt. Mehr Hits = die LLM hat reicheren Kontext, aber der Prompt wird größer und das Modell kann ablenkbar werden. 10 ist eine solide Mitte für die meisten Domains."
						/>
					}
					value={topHits}
					onChange={(v) => setTopHits(typeof v === "number" ? v : Number(v) || 0)}
					min={1}
					max={50}
					step={1}
					description="Anzahl Schema-Chunks, die oos_schema_search der LLM liefert. Mehr = reicherer Kontext, größerer Prompt."
				/>

				<Group justify="space-between" align="center">
					<Button variant="subtle" onClick={() => void resetDefaults()}>
						Auf Defaults zurücksetzen
					</Button>
					<Group gap="md" align="center">
						{savedAt && (
							<Text size="xs" c="dimmed">
								Gespeichert um {savedAt}
							</Text>
						)}
						<Button onClick={() => void submit()}>Save</Button>
					</Group>
				</Group>
			</Stack>
		</Paper>
	);
}

// ─── Helpers ─────────────────────────────────────────────────────────

/**
 * LabelWithHelp wraps a form label so a small help icon sits to its
 * right. The icon hovers a Mantine Tooltip with the longer
 * "why does this matter" copy.
 *
 * Mantine 9 lets us pass any ReactNode as the `label` prop on its
 * inputs, so this composes cleanly without reaching for the input's
 * internal styling.
 *
 * `strong` controls whether the label text is rendered bold. The
 * Slider's manual label needs strong=true to match the surrounding
 * field labels which Mantine bolds by default.
 */
function LabelWithHelp({
	text,
	help,
	strong = false,
}: {
	text:    string;
	help:    string;
	strong?: boolean;
}): ReactNode {
	return (
		<Box
			component="span"
			style={{
				display: "inline-flex",
				alignItems: "center",
				gap: 6,
			}}
		>
			<Text
				component="span"
				size="sm"
				fw={strong ? 500 : undefined}
			>
				{text}
			</Text>
			<Tooltip
				label={help}
				multiline
				w={320}
				withArrow
				position="right"
				openDelay={150}
			>
				<ActionIcon
					variant="subtle"
					color="gray"
					size="xs"
					aria-label={`${text}: Erklärung`}
					// Don't toggle a parent label's "for" focus.
					onClick={(e) => e.preventDefault()}
				>
					<IconHelpCircle size={14} />
				</ActionIcon>
			</Tooltip>
		</Box>
	);
}

/**
 * describePreset renders a one-line summary of how the current form
 * values compare to the named presets. Lets the user see "you're
 * close to Local but with cloud-style timeout" without reading
 * every field. Returns undefined when nothing useful to say so the
 * description line collapses.
 */
function describePreset(
	temperature: number,
	timeoutText: string,
	maxTokens:   number,
	topHits:     number,
): string {
	const ms = parseTimeout(timeoutText);
	const matches = (p: FinetuningSettings): boolean =>
		p.temperature === temperature &&
		p.timeoutMs === ms &&
		p.maxTokens === maxTokens &&
		p.topHits   === topHits;

	for (const [name, preset] of Object.entries(FINETUNING_PRESETS)) {
		if (matches(preset)) {
			return `Aktuell: ${name === "cloud" ? "Cloud" : "Lokal"} — ${
				PRESET_DESCRIPTIONS[name as FinetuningPreset]
			}`;
		}
	}
	return "Eigene Werte. Wähle ein Preset, um einen sauberen Startpunkt zu setzen.";
}

// ─── Timeout parsing ─────────────────────────────────────────────────

/**
 * formatTimeout renders milliseconds as a Go-style duration string.
 * Picks the largest unit that fits without fractions, falling back
 * to a combined "5m0s" form for the very common 5-minute default.
 */
function formatTimeout(ms: number): string {
	if (!Number.isFinite(ms) || ms <= 0) return "5m";
	const totalSec = Math.round(ms / 1000);
	const h = Math.floor(totalSec / 3600);
	const m = Math.floor((totalSec % 3600) / 60);
	const s = totalSec % 60;
	if (h > 0 && m === 0 && s === 0) return `${h}h`;
	if (h === 0 && m > 0 && s === 0) return `${m}m`;
	if (h === 0 && m === 0)          return `${s}s`;
	if (h === 0)                     return `${m}m${s}s`;
	return `${h}h${m}m${s}s`;
}

/**
 * parseTimeout decodes a Go-style duration like "30s", "5m", "5m0s",
 * "1h" into milliseconds. Returns null on malformed input so the
 * caller can render a validation error.
 */
function parseTimeout(text: string): number | null {
	const trimmed = text.trim().toLowerCase();
	if (!trimmed) return null;
	if (/^\d+$/.test(trimmed)) {
		// bare number — interpret as seconds, like Go does for some flags
		const n = Number(trimmed);
		return Number.isFinite(n) ? n * 1000 : null;
	}
	const re = /(\d+)\s*(h|m|s)/g;
	let total = 0;
	let matched = false;
	let m: RegExpExecArray | null;
	while ((m = re.exec(trimmed)) !== null) {
		matched = true;
		const n = Number(m[1]);
		if (!Number.isFinite(n)) return null;
		const unit = m[2];
		if      (unit === "h") total += n * 3600 * 1000;
		else if (unit === "m") total += n * 60 * 1000;
		else if (unit === "s") total += n * 1000;
	}
	if (!matched) return null;
	if (total <= 0) return null;
	return total;
}
