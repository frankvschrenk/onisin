// SettingsConnectionPanel.tsx — Connection settings as a tab body.
//
// The panel that used to live in the SettingsDrawer. It owns the
// LLM endpoint, key, model, and the NATS URL for backend services.
//
// Differences vs. the old drawer:
//   - No open/close lifecycle: the tab is always "open" while the
//     Settings group is alive, so the form just reads the persisted
//     row on mount and saves on Save.
//   - Layout breathes a little: a tab gets the full right column,
//     not a 360-px drawer. Group the form into Connection / Backend
//     sections via clear dividers.
//
// Persistence and the Save button still go through useAppSettings,
// the same hook the legacy drawer used. Save also flows through the
// process-wide pub/sub bus so the Footer's model badge updates.

import { useCallback, useEffect, useState } from "react";
import {
	Alert,
	Button,
	Divider,
	Group,
	PasswordInput,
	Paper,
	Select,
	Stack,
	Text,
	TextInput,
	Title,
	Tooltip,
} from "@mantine/core";
import { IconAlertCircle, IconRefresh } from "@tabler/icons-react";

import { listModels }    from "../llm/models";
import { getLocalNodeId } from "../store/node-id";
import {
	DEFAULT_APP_SETTINGS,
	useAppSettings,
	type AppSettings,
} from "../store/settings";

const REFRESH_DEBOUNCE_MS = 400;

export function SettingsConnectionPanel() {
	const { settings, loaded, save } = useAppSettings();

	const [llmBaseUrl, setLlmBaseUrl] = useState(DEFAULT_APP_SETTINGS.llmBaseUrl);
	const [llmApiKey,  setLlmApiKey]  = useState(DEFAULT_APP_SETTINGS.llmApiKey);
	const [llmModel,   setLlmModel]   = useState(DEFAULT_APP_SETTINGS.llmModel);
	const [natsUrl,    setNatsUrl]    = useState(DEFAULT_APP_SETTINGS.natsUrl);

	const [s3AccessKey, setS3AccessKey] = useState(DEFAULT_APP_SETTINGS.s3AccessKey);
	const [s3SecretKey, setS3SecretKey] = useState(DEFAULT_APP_SETTINGS.s3SecretKey);

	const [models,        setModels]   = useState<string[]>([]);
	const [modelsLoading, setMLoading] = useState(false);
	const [modelsError,   setMError]   = useState<string | null>(null);

	const [savedAt, setSavedAt] = useState<string | null>(null);

	// Re-seed the form once the persisted row arrives (and again on
	// every external save propagated through the settings bus).
	useEffect(() => {
		if (!loaded) return;
		setLlmBaseUrl(settings.llmBaseUrl);
		setLlmApiKey(settings.llmApiKey);
		setLlmModel(settings.llmModel);
		setNatsUrl(settings.natsUrl);
		setS3AccessKey(settings.s3AccessKey);
		setS3SecretKey(settings.s3SecretKey);
	}, [loaded, settings]);

	const refreshModels = useCallback(
		async (base: string, key: string) => {
			setMLoading(true);
			setMError(null);
			try {
				const ids = await listModels(base, key);
				setModels(ids);
				if (ids.length > 0 && !ids.includes(llmModel)) {
					setLlmModel("");
				}
			} catch (err) {
				setModels([]);
				setMError(err instanceof Error ? err.message : String(err));
			} finally {
				setMLoading(false);
			}
		},
		[llmModel],
	);

	// Auto-refresh whenever the endpoint or key changes.
	useEffect(() => {
		const handle = setTimeout(() => {
			void refreshModels(llmBaseUrl, llmApiKey);
		}, REFRESH_DEBOUNCE_MS);
		return () => clearTimeout(handle);
	}, [llmBaseUrl, llmApiKey, refreshModels]);

	const submit = async () => {
		// Spread existing settings first so fields not edited by this panel
		// (auth*) survive a Save. Only this panel's controlled fields override.
		const next: AppSettings = {
			...settings,
			llmBaseUrl,
			llmApiKey,
			llmModel,
			natsUrl,
			s3AccessKey,
			s3SecretKey,
		};
		await save(next);
		setSavedAt(new Date().toLocaleTimeString());
	};

	return (
		<Paper p="lg" radius={0} style={{ height: "100%", overflow: "auto" }}>
			<Stack gap="lg" maw={640}>
				<div>
					<Title order={3}>Connection</Title>
					<Text size="sm" c="dimmed" mt={4}>
						Anything that speaks the OpenAI-compatible API works
						here — vLLM, OpenAI, Anthropic via a proxy, or any local endpoint.
					</Text>
				</div>

				<Stack gap="sm">
					<TextInput
						label="LLM base URL"
						placeholder="http://localhost:11434"
						value={llmBaseUrl}
						onChange={(e) => setLlmBaseUrl(e.currentTarget.value)}
					/>
					<PasswordInput
						label="API key"
						placeholder="leave blank for local endpoints"
						value={llmApiKey}
						onChange={(e) => setLlmApiKey(e.currentTarget.value)}
					/>
					<Group align="flex-end" gap="xs" wrap="nowrap">
						<Select
							label="Chat model"
							placeholder={
								modelsLoading
									? "loading…"
									: models.length === 0
										? "no models found"
										: "select a model"
							}
							value={llmModel || null}
							onChange={(v) => setLlmModel(v ?? "")}
							data={models}
							searchable
							nothingFoundMessage="No matching model"
							style={{ flex: 1 }}
							disabled={modelsLoading || models.length === 0}
						/>
						<Tooltip label="Refresh model list" withArrow>
							<Button
								variant="default"
								onClick={() => void refreshModels(llmBaseUrl, llmApiKey)}
								loading={modelsLoading}
								aria-label="Refresh model list"
								px="xs"
							>
								<IconRefresh size={16} />
							</Button>
						</Tooltip>
					</Group>
					{modelsError && (
						<Alert
							color="red"
							variant="light"
							icon={<IconAlertCircle size={16} />}
							p="xs"
						>
							<Text size="xs">{modelsError}</Text>
						</Alert>
					)}
				</Stack>

				<Divider label="Backend" labelPosition="left" />

				<Stack gap="sm">
					<TextInput
						label="NATS URL"
						placeholder="nats://localhost:4222"
						value={natsUrl}
						onChange={(e) => setNatsUrl(e.currentTarget.value)}
					/>
					{getLocalNodeId() && (
						<Tooltip label={getLocalNodeId()} position="top" withArrow>
							<TextInput
								label="Node ID"
								value={getLocalNodeId().slice(0, 8) + "…" + getLocalNodeId().slice(-4)}
								readOnly
								styles={{ input: { fontFamily: "monospace", cursor: "default" } }}
								description="Stable Ed25519 identity for this instance. Hover for full ID."
								onClick={() => void navigator.clipboard?.writeText(getLocalNodeId())}
							/>
						</Tooltip>
					)}
				</Stack>

				<Divider label="Object storage credentials" labelPosition="left" />

				<Stack gap="sm">
					<Text size="xs" c="dimmed">
						Used by pipelines with <code>source docs s3=&quot;...&quot;</code>. The S3 endpoint and region
						are configured on the oosduck service; only your credentials live here, stored in the OS keychain.
					</Text>
					<TextInput
						label="Access key"
						placeholder="e.g. minioadmin"
						value={s3AccessKey}
						onChange={(e) => setS3AccessKey(e.currentTarget.value)}
					/>
					<PasswordInput
						label="Secret key"
						placeholder="keychain-protected"
						value={s3SecretKey}
						onChange={(e) => setS3SecretKey(e.currentTarget.value)}
					/>
				</Stack>

				<Group justify="flex-end" align="center" gap="md">
					{savedAt && (
						<Text size="xs" c="dimmed">
							Gespeichert um {savedAt}
						</Text>
					)}
					<Button onClick={() => void submit()} disabled={!llmBaseUrl && !natsUrl}>
						Save
					</Button>
				</Group>
			</Stack>
		</Paper>
	);
}
