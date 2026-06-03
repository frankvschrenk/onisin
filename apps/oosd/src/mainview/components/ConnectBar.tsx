// ConnectBar.tsx — header strip with the DSN and one state-aware
// connect/disconnect button. Designed to live inside an
// AppShell.Header.
//
// The button replaces what was previously a separate Verbinden
// button + a status badge. Its label and colour reflect the
// current Status; clicking it does the opposite of whatever the
// state is currently showing:
//
//   idle / error  → blue,  label "Verbinden",       click connects
//   connecting    → blue,  loading spinner          (disabled)
//   connected     → teal,  label "Verbunden"        — hover flips it
//                          to red "Trennen", click disconnects
//
// Errors no longer get their own badge: the message is shown as a
// small red caption next to the button so the user can read it
// without giving up half the header to a multi-line label.

import { useState } from "react";
import { Button, Group, Text, TextInput, Tooltip } from "@mantine/core";

import type { Status } from "../types";

export function ConnectBar({
	dsn,
	onDsnChange,
	status,
	onConnect,
	onDisconnect,
}: {
	dsn: string;
	onDsnChange: (v: string) => void;
	status: Status;
	onConnect: () => void;
	onDisconnect: () => void;
}) {
	const [hover, setHover] = useState(false);
	const isConnected  = status.kind === "connected";
	const isConnecting = status.kind === "connecting";

	// Pre-compute the label/color/handler trio so the JSX below stays
	// linear and the state machine reads top-to-bottom.
	let label:   string;
	let color:   string;
	let handler: () => void;

	if (isConnecting) {
		label   = "Verbinde…";
		color   = "blue";
		handler = () => {};
	} else if (isConnected) {
		if (hover) {
			label   = "Trennen";
			color   = "red";
			handler = onDisconnect;
		} else {
			label   = "Verbunden";
			color   = "teal";
			handler = onDisconnect;
		}
	} else {
		label   = "Verbinden";
		color   = "blue";
		handler = onConnect;
	}

	const button = (
		<Button
			size="sm"
			color={color}
			loading={isConnecting}
			onClick={handler}
			onMouseEnter={() => setHover(true)}
			onMouseLeave={() => setHover(false)}
			style={{ minWidth: 120 }}
		>
			{label}
		</Button>
	);

	return (
		<Group gap="sm" px="md" h="100%" align="center" wrap="nowrap">
			<TextInput
				size="sm"
				style={{ flex: 1, fontFamily: "ui-monospace, Menlo, monospace" }}
				value={dsn}
				onChange={(e) => onDsnChange(e.currentTarget.value)}
				placeholder="postgres://..."
				disabled={isConnected || isConnecting}
			/>
			{status.kind === "error" && (
				<Text size="xs" c="red" style={{ maxWidth: 240 }} truncate>
					{status.message}
				</Text>
			)}
			{isConnected ? (
				<Tooltip label="Klick zum Trennen" position="bottom" withArrow>
					{button}
				</Tooltip>
			) : (
				button
			)}
		</Group>
	);
}
