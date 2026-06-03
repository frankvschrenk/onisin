// IamPanel.tsx — User & group admin for the built-in oosiam IdP.
//
// Two columns: left the user list (New / Refresh, delete per row),
// right the selected user's detail — read-only email/username, an
// editable group list, and a set-password action. All operations go
// over NATS (oos.cmd.oosiam.user.*) via the bun iam-rpc module; oosiam
// is the command handler on the other end.
//
// Groups carry the authorization contract: oos derives a role by
// splitting each group name on "-" and ranking admin > manager > user
// (see oos/src/bun/auth-claims.ts), so the group input hints at the
// "oos-admin" shape rather than leaving it free-text.

import { useCallback, useEffect, useState } from "react";
import {
	Alert, Box, Button, Divider, Group, Modal, PasswordInput,
	ScrollArea, Stack, TagsInput, Text, TextInput, Title, UnstyledButton,
} from "@mantine/core";
import {
	IconKey, IconPlus, IconRefresh, IconTrash, IconUser, IconUserPlus,
} from "@tabler/icons-react";
import { rpc } from "../rpc";
import type { IamUser } from "../rpc";

const GROUP_HINT = "Role comes from the group name split on \"-\": admin > manager > user. e.g. oos-admin";

export function IamPanel({ disabled }: { disabled: boolean }) {
	const [users,      setUsers]      = useState<IamUser[]>([]);
	const [loading,    setLoading]    = useState(false);
	const [selectedId, setSelectedId] = useState<number | null>(null);
	const [error,      setError]      = useState<string | null>(null);

	// New-user dialog
	const [newOpen,   setNewOpen]   = useState(false);
	const [nEmail,    setNEmail]    = useState("");
	const [nUsername, setNUsername] = useState("");
	const [nPassword, setNPassword] = useState("");
	const [nGroups,   setNGroups]   = useState<string[]>(["oos-user"]);
	const [newBusy,   setNewBusy]   = useState(false);

	// Group editor
	const [groupsDraft, setGroupsDraft] = useState<string[]>([]);
	const [groupsBusy,  setGroupsBusy]  = useState(false);

	// Password reset
	const [pwValue, setPwValue] = useState("");
	const [pwBusy,  setPwBusy]  = useState(false);
	const [pwOk,    setPwOk]    = useState(false);

	// Delete confirm
	const [deleteTarget, setDeleteTarget] = useState<IamUser | null>(null);

	const selected = users.find((u) => u.id === selectedId) ?? null;

	const loadUsers = useCallback(async () => {
		setLoading(true);
		setError(null);
		try {
			const res = await rpc.listIamUsers({});
			if (res.error) setError(res.error);
			else setUsers(res.users);
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => { void loadUsers(); }, [loadUsers]);

	function selectUser(u: IamUser) {
		setSelectedId(u.id);
		setGroupsDraft(u.groups);
		setPwValue("");
		setPwOk(false);
		setError(null);
	}

	async function onCreate() {
		if (!nEmail.trim() || !nPassword) return;
		setNewBusy(true);
		try {
			const res = await rpc.createIamUser({
				email:    nEmail.trim(),
				username: nUsername.trim() || nEmail.trim(),
				password: nPassword,
				groups:   nGroups,
			});
			if (!res.ok) { setError(res.error ?? "create failed"); return; }
			setNewOpen(false);
			setNEmail(""); setNUsername(""); setNPassword(""); setNGroups(["oos-user"]);
			await loadUsers();
			if (res.user) setSelectedId(res.user.id);
		} finally {
			setNewBusy(false);
		}
	}

	async function onSaveGroups() {
		if (!selected) return;
		setGroupsBusy(true);
		try {
			const res = await rpc.setIamUserGroups({ id: selected.id, groups: groupsDraft });
			if (!res.ok) { setError(res.error ?? "save failed"); return; }
			await loadUsers();
		} finally {
			setGroupsBusy(false);
		}
	}

	async function onSetPassword() {
		if (!selected || !pwValue) return;
		setPwBusy(true);
		try {
			const res = await rpc.setIamUserPassword({ id: selected.id, password: pwValue });
			if (!res.ok) { setError(res.error ?? "set password failed"); return; }
			setPwValue("");
			setPwOk(true);
		} finally {
			setPwBusy(false);
		}
	}

	async function onDelete(u: IamUser) {
		try {
			const res = await rpc.deleteIamUser({ id: u.id });
			if (!res.ok) { setError(res.error ?? "delete failed"); return; }
			if (selectedId === u.id) setSelectedId(null);
			await loadUsers();
		} finally {
			setDeleteTarget(null);
		}
	}

	return (
		<Box style={{ display: "flex", height: "100%", overflow: "hidden" }}>

			{/* ── Left: user list ── */}
			<Box style={{ width: 280, borderRight: "1px solid var(--mantine-color-default-border)", display: "flex", flexDirection: "column" }}>
				<Group p="xs" justify="space-between">
					<Text fw={600} size="sm">Users</Text>
					<Group gap={4}>
						<Button size="compact-xs" variant="subtle" leftSection={<IconRefresh size={12} />}
							loading={loading} onClick={() => void loadUsers()} disabled={disabled}>
							Refresh
						</Button>
						<Button size="compact-xs" variant="subtle" leftSection={<IconPlus size={12} />}
							onClick={() => setNewOpen(true)} disabled={disabled}>
							New
						</Button>
					</Group>
				</Group>
				<Divider />
				{error && <Alert color="red" p="xs" m="xs">{error}</Alert>}
				<ScrollArea style={{ flex: 1 }}>
					<Stack gap={0} p="xs">
						{users.length === 0 && !loading && (
							<Text size="xs" c="dimmed">No users yet.</Text>
						)}
						{users.map((u) => (
							<UnstyledButton key={u.id}
								onClick={() => selectUser(u)}
								style={{
									padding: "6px 8px",
									borderRadius: 4,
									background: selectedId === u.id ? "var(--mantine-color-blue-light)" : undefined,
								}}>
								<Group justify="space-between" wrap="nowrap">
									<Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
										<IconUser size={14} />
										<Stack gap={0} style={{ minWidth: 0 }}>
											<Text size="xs" fw={selectedId === u.id ? 600 : 400} truncate>{u.username}</Text>
											<Text size="xs" c="dimmed" truncate>{u.email}</Text>
										</Stack>
									</Group>
									<UnstyledButton onClick={(e) => { e.stopPropagation(); setDeleteTarget(u); }}
										style={{ color: "var(--mantine-color-red-6)", display: "flex" }}>
										<IconTrash size={12} />
									</UnstyledButton>
								</Group>
							</UnstyledButton>
						))}
					</Stack>
				</ScrollArea>
			</Box>

			{/* ── Right: detail ── */}
			<Box style={{ flex: 1, overflow: "auto" }}>
				{selected ? (
					<Stack p="md" gap="lg" style={{ maxWidth: 520 }}>
						<Stack gap={2}>
							<Title order={4}>{selected.username}</Title>
							<Text size="sm" c="dimmed">{selected.email}</Text>
						</Stack>

						<Stack gap="xs">
							<Text fw={600} size="sm">Groups</Text>
							<TagsInput
								value={groupsDraft}
								onChange={setGroupsDraft}
								placeholder="add a group"
								description={GROUP_HINT}
								disabled={disabled}
							/>
							<Group justify="flex-end">
								<Button size="compact-sm" loading={groupsBusy}
									onClick={() => void onSaveGroups()} disabled={disabled}>
									Save groups
								</Button>
							</Group>
						</Stack>

						<Divider />

						<Stack gap="xs">
							<Text fw={600} size="sm">Set password</Text>
							<PasswordInput
								value={pwValue}
								onChange={(e) => { setPwValue(e.currentTarget.value); setPwOk(false); }}
								placeholder="new password"
								disabled={disabled}
							/>
							<Group justify="space-between">
								{pwOk ? <Text size="xs" c="green">Password updated.</Text> : <span />}
								<Button size="compact-sm" leftSection={<IconKey size={14} />}
									loading={pwBusy} onClick={() => void onSetPassword()}
									disabled={disabled || !pwValue}>
									Update
								</Button>
							</Group>
						</Stack>
					</Stack>
				) : (
					<Box p="md"><Text size="xs" c="dimmed">Select a user.</Text></Box>
				)}
			</Box>

			{/* ── New user dialog ── */}
			<Modal opened={newOpen} onClose={() => setNewOpen(false)} title="New user" size="sm">
				<Stack>
					<TextInput label="Email" placeholder="user@example.com" value={nEmail}
						onChange={(e) => setNEmail(e.currentTarget.value)} />
					<TextInput label="Username" placeholder="defaults to email" value={nUsername}
						onChange={(e) => setNUsername(e.currentTarget.value)} />
					<PasswordInput label="Password" value={nPassword}
						onChange={(e) => setNPassword(e.currentTarget.value)} />
					<TagsInput label="Groups" value={nGroups} onChange={setNGroups}
						description={GROUP_HINT} placeholder="add a group" />
					<Button leftSection={<IconUserPlus size={16} />}
						loading={newBusy} onClick={() => void onCreate()}
						disabled={!nEmail.trim() || !nPassword}>
						Create
					</Button>
				</Stack>
			</Modal>

			{/* ── Delete confirm ── */}
			<Modal opened={!!deleteTarget} onClose={() => setDeleteTarget(null)} title="Delete user" size="sm">
				<Stack>
					<Text size="sm">Delete user <b>{deleteTarget?.email}</b>? This cannot be undone.</Text>
					<Group justify="flex-end">
						<Button variant="default" onClick={() => setDeleteTarget(null)}>Cancel</Button>
						<Button color="red" onClick={() => deleteTarget && void onDelete(deleteTarget)}>Delete</Button>
					</Group>
				</Stack>
			</Modal>

		</Box>
	);
}
