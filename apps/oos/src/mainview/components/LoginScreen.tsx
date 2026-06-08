// LoginScreen.tsx — Full-screen login gate for oos.
//
// Phase machine:
//   checking   — waiting for getInitialState RPC
//   setup      — IAM not configured, show config form
//   ready      — IAM configured, show login button
//   logging-in — PKCE flow in progress (browser open)
//   done       — logged in, hidden by App

import { useEffect, useState } from "react";
import {
	Box, Button, Center, Loader, Stack,
	Text, TextInput, Title,
} from "@mantine/core";
import { rpc, setAuthCompletedHandler } from "../rpc";
import { setClaims }       from "../store/auth";
import { setLocalNodeId } from "../store/node-id";
import type { AppSettings } from "../store/settings";

type Phase =
	| { kind: "checking" }
	| { kind: "setup" }
	| { kind: "ready" }
	| { kind: "logging-in" }
	| { kind: "done" };

export function LoginScreen({ onDone }: { onDone: () => void }) {
	const [phase,       setPhase]      = useState<Phase>({ kind: "checking" });
	const [settings,   setSettings]   = useState<AppSettings | null>(null);
	const [error,      setError]      = useState("");
	const [issuerUrl,  setIssuerUrl]  = useState("http://localhost:5556");
	const [clientId,   setClientId]   = useState("oos-desktop");
	const [redirectUri,setRedirectUri] = useState("http://localhost:5557/callback");

	// Single RPC call on mount — Bun already computed everything at startup.
	useEffect(() => {
		void rpc.getInitialState({}).then((state) => {
			setSettings(state.settings);
			if (state.nodeId) setLocalNodeId(state.nodeId);
			if (state.hasToken) {
				if (state.accessToken) setClaims(state.accessToken);
				setPhase({ kind: "done" });
				onDone();
			} else if (state.iamConfigured) {
				setIssuerUrl(state.settings.authIssuerUrl);
				setClientId(state.settings.authClientId);
				setRedirectUri(state.settings.authRedirectUri);
				setPhase({ kind: "ready" });
			} else {
				setPhase({ kind: "setup" });
			}
		});
	}, []); // eslint-disable-line react-hooks/exhaustive-deps

	// Bun sends authCompleted when PKCE callback succeeds.
	useEffect(() => {
		setAuthCompletedHandler(() => {
			setPhase({ kind: "done" });
			onDone();
		});
	}, []); // eslint-disable-line react-hooks/exhaustive-deps

	async function handleSaveIAM() {
		if (!settings) return;
		const next = {
			...settings,
			authIssuerUrl:   issuerUrl,
			authClientId:    clientId,
			authRedirectUri: redirectUri,
		};
		await rpc.saveSettings(next);
		setPhase({ kind: "ready" });
	}

	async function handleLogin() {
		setError("");
		setPhase({ kind: "logging-in" });
		const result = await rpc.startLogin({});
		if (!result.ok) {
			setError(result.error ?? "Login failed");
			setPhase({ kind: "ready" });
			return;
		}
		if (result.url) await rpc.openExternalUrl({ url: result.url });
	}

	if (phase.kind === "checking" || phase.kind === "done") {
		return (
			<Center h="100vh">
				<Loader />
			</Center>
		);
	}

	return (
		<Center h="100vh" bg="light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-8))">
			<Box p="xl" miw={400} style={{
				background: "light-dark(var(--mantine-color-white), var(--mantine-color-dark-6))",
				borderRadius: 8,
				boxShadow: "0 2px 16px rgba(0,0,0,0.2)",
			}}>
				<Stack gap="md">
					<Title order={3}>Onisin OS</Title>

					{phase.kind === "setup" && (
						<>
							<Text size="sm" c="dimmed">
								Bitte zuerst die IAM-Einstellungen konfigurieren,
								bevor Sie sich anmelden.
							</Text>
							<TextInput label="IAM Issuer URL"
								placeholder="http://localhost:5556"
								value={issuerUrl}
								onChange={(e) => setIssuerUrl(e.currentTarget.value)} />
							<TextInput label="Client ID"
								value={clientId}
								onChange={(e) => setClientId(e.currentTarget.value)} />
							<TextInput label="Redirect URI"
								value={redirectUri}
								onChange={(e) => setRedirectUri(e.currentTarget.value)} />
							<Button onClick={() => void handleSaveIAM()}
								disabled={!issuerUrl}>
								Speichern & Weiter
							</Button>
						</>
					)}

					{phase.kind === "ready" && (
						<>
							<Text size="sm" c="dimmed">
								IAM: <strong>{issuerUrl}</strong>
							</Text>
							{error && <Text size="sm" c="red">{error}</Text>}
							<Button onClick={() => void handleLogin()}>Anmelden</Button>
							<Button variant="subtle" size="xs"
								onClick={() => setPhase({ kind: "setup" })}>
								IAM-Einstellungen ändern
							</Button>
						</>
					)}

					{phase.kind === "logging-in" && (
						<>
							<Loader size="sm" />
							<Text size="sm" c="dimmed">
								Browser geöffnet — bitte im Browser anmelden.
							</Text>
							<Button variant="subtle" size="xs"
								onClick={() => setPhase({ kind: "ready" })}>
								Abbrechen
							</Button>
						</>
					)}
				</Stack>
			</Box>
		</Center>
	);
}
