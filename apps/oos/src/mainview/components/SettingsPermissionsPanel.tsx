// SettingsPermissionsPanel.tsx — Current user role + domain permission matrix.
//
// Shows:
//   - Username and resolved role from the stored token
//   - Per-domain table: which actions (read / write / delete) the
//     current role has. Useful for understanding why a Save or Delete
//     was blocked.

import { useEffect, useState } from "react";
import {
	Alert,
	Badge,
	Box,
	Center,
	Group,
	Loader,
	Stack,
	Table,
	Text,
	Title,
} from "@mantine/core";
import { IconAlertCircle, IconCheck, IconX } from "@tabler/icons-react";
import { rpc } from "../rpc";

interface DomainPermission {
	domain:  string;
	actions: string[];
}

type Stage =
	| { kind: "loading" }
	| { kind: "error"; message: string }
	| { kind: "ready"; role: string; username: string; groups: string[]; permissions: DomainPermission[] };

export function SettingsPermissionsPanel() {
	const [stage, setStage] = useState<Stage>({ kind: "loading" });

	useEffect(() => {
		void rpc.getMyPermissions({}).then((result) => {
			if (result.error) {
				setStage({ kind: "error", message: result.error });
				return;
			}
			if (!result.role) {
				setStage({ kind: "error", message: "Nicht angemeldet oder keine Rolle zugewiesen." });
				return;
			}
			setStage({
				kind:        "ready",
				role:        result.role,
				username:    result.username,
				permissions: result.permissions,
			groups:      result.groups ?? [],
			});
		}).catch((e: unknown) => {
			setStage({ kind: "error", message: String(e) });
		});
	}, []);

	if (stage.kind === "loading") {
		return (
			<Center style={{ height: "100%" }}>
				<Stack align="center" gap="xs">
					<Loader type="dots" size="sm" />
					<Text size="xs" c="dimmed">Berechtigungen werden geladen…</Text>
				</Stack>
			</Center>
		);
	}

	if (stage.kind === "error") {
		return (
			<Box p="lg">
				<Alert color="red" variant="light" icon={<IconAlertCircle size={16} />}
					title="Berechtigungen konnten nicht geladen werden">
					<Text size="sm">{stage.message}</Text>
				</Alert>
			</Box>
		);
	}

	const { role, username, groups, permissions: rawPermissions } = stage;
	// Deduplicate by domain name — oosgql may expose the same domain
	// under multiple schemas (e.g. public.person + oos.person).
	const seen = new Set<string>();
	const permissions = rawPermissions.filter((p) => {
		if (seen.has(p.domain)) return false;
		seen.add(p.domain);
		return true;
	});
	const roleColor =
		role === "admin"   ? "red" :
		role === "manager" ? "orange" :
		                     "gray";

	return (
		<Box p="lg">
			<Stack gap="lg">
				{/* Identity */}
				<Group gap="sm" align="center">
					<Title order={5}>Angemeldeter Benutzer</Title>
				</Group>
				<Group gap="sm" align="center">
					<Text size="sm" fw={500}>{username || "—"}</Text>
					<Badge color={roleColor} variant="light" size="sm">{role}</Badge>
				</Group>
				{groups.length > 0 && (
					<Group gap="xs">
						<Text size="xs" c="dimmed">IAM-Gruppen:</Text>
						{groups.map((g) => (
							<Badge key={g} size="xs" variant="outline" color="gray">{g}</Badge>
						))}
					</Group>
				)}

				{/* Permission matrix */}
				<Title order={5}>Zugriffsrechte</Title>
				{permissions.length === 0 ? (
					<Text size="sm" c="dimmed">
						Keine Domains verfügbar oder keine Berechtigungen für diese Rolle.
					</Text>
				) : (
					<Table withTableBorder withColumnBorders striped>
						<Table.Thead>
							<Table.Tr>
								<Table.Th>Domain</Table.Th>
								<Table.Th style={{ textAlign: "center", width: 80 }}>Lesen</Table.Th>
								<Table.Th style={{ textAlign: "center", width: 80 }}>Schreiben</Table.Th>
								<Table.Th style={{ textAlign: "center", width: 80 }}>Löschen</Table.Th>
							</Table.Tr>
						</Table.Thead>
						<Table.Tbody>
							{permissions.map((p) => (
								<Table.Tr key={p.domain}>
									<Table.Td>
										<Text size="sm" fw={500}>{p.domain}</Text>
									</Table.Td>
									<Table.Td style={{ textAlign: "center" }}>
										<ActionIcon has={p.actions.includes("read")} />
									</Table.Td>
									<Table.Td style={{ textAlign: "center" }}>
										<ActionIcon has={p.actions.includes("write")} />
									</Table.Td>
									<Table.Td style={{ textAlign: "center" }}>
										<ActionIcon has={p.actions.includes("delete")} />
									</Table.Td>
								</Table.Tr>
							))}
						</Table.Tbody>
					</Table>
				)}
			</Stack>
		</Box>
	);
}

function ActionIcon({ has }: { has: boolean }) {
	return has
		? <IconCheck  size={16} color="var(--mantine-color-green-6)" />
		: <IconX      size={16} color="var(--mantine-color-red-3)"   />;
}
