// IdList.tsx — middle column showing all ids in the active table.
//
// Each row carries a delete affordance that fades in on hover (and
// stays visible while the row is selected). The header has two
// icons: a plus for creating a new row, and the existing refresh.
//
// Deletion goes through a Mantine confirmation modal because rows
// in oos.domain are referenced from oos.view; deleting a domain
// without taking note of which views break is the kind of accident
// that costs minutes to recover from.

import {
	ActionIcon,
	Group,
	Modal,
	NavLink,
	ScrollArea,
	Stack,
	Text,
	Button,
} from "@mantine/core";
import { useState } from "react";

import type { Kind } from "../types";

export function IdList({
	kind,
	ids,
	selected,
	onSelect,
	onRefresh,
	onNew,
	onDelete,
	disabled,
}: {
	kind: Kind;
	ids: string[];
	selected: string | null;
	onSelect: (id: string) => void;
	onRefresh: () => void;
	onNew: () => void;
	onDelete: (id: string) => Promise<{ ok: boolean; error?: string }>;
	disabled: boolean;
}) {
	const heading = kind === "domain" ? "Domain" : "View";
	const [hoveredId, setHoveredId] = useState<string | null>(null);
	const [pendingDelete, setPendingDelete] = useState<string | null>(null);
	const [deleting, setDeleting] = useState(false);

	async function confirmDelete() {
		if (!pendingDelete) return;
		setDeleting(true);
		try {
			const res = await onDelete(pendingDelete);
			if (res.ok) setPendingDelete(null);
			// Errors stay on screen until the user explicitly cancels —
			// the alert in the modal is the feedback channel.
		} finally {
			setDeleting(false);
		}
	}

	return (
		<Stack gap={0} h="100%">
			<Group
				justify="space-between"
				align="center"
				px="sm"
				py={6}
				style={{ borderBottom: "1px solid var(--mantine-color-default-border)" }}
			>
				<Text size="sm" fw={500}>{heading}</Text>
				<Group gap={4}>
					<ActionIcon
						variant="subtle"
						size="sm"
						onClick={onNew}
						disabled={disabled}
						aria-label="New"
					>
						+
					</ActionIcon>
					<ActionIcon
						variant="subtle"
						size="sm"
						onClick={onRefresh}
						disabled={disabled}
						aria-label="Refresh"
					>
						↻
					</ActionIcon>
				</Group>
			</Group>

			<ScrollArea style={{ flex: 1 }}>
				{ids.length === 0 ? (
					<Text size="xs" c="dimmed" px="md" py="xs">
						{disabled ? "not connected" : "empty"}
					</Text>
				) : (
					<Stack gap={0} p={4}>
						{ids.map((id) => {
							const showDelete = hoveredId === id || selected === id;
							return (
								<NavLink
									key={id}
									label={id}
									active={selected === id}
									onClick={() => onSelect(id)}
									onMouseEnter={() => setHoveredId(id)}
									onMouseLeave={() =>
										setHoveredId((current) => (current === id ? null : current))
									}
									rightSection={
										<ActionIcon
											component="span"
											variant="subtle"
											color="red"
											size="xs"
											style={{
												opacity: showDelete ? 1 : 0,
												transition: "opacity 120ms ease",
											}}
											onClick={(e) => {
												e.stopPropagation();
												setPendingDelete(id);
											}}
											aria-label={`Delete ${id}`}
										>
											×
										</ActionIcon>
									}
								/>
							);
						})}
					</Stack>
				)}
			</ScrollArea>

			<Modal
				opened={pendingDelete !== null}
				onClose={() => !deleting && setPendingDelete(null)}
				title={`Delete ${kind}?`}
				centered
				size="sm"
			>
				<Stack gap="md">
					<Text size="sm">
						This will permanently delete <Text component="span" fw={700}>
							{pendingDelete}
						</Text> from <Text component="span" ff="monospace">
							{kind === "domain" ? "oos.domain" : "oos.view"}
						</Text>.
					</Text>
					<Text size="xs" c="dimmed">
						Other rows that reference this {kind} will break until you
						update them.
					</Text>
					<Group justify="flex-end">
						<Button
							variant="subtle"
							onClick={() => setPendingDelete(null)}
							disabled={deleting}
						>
							Cancel
						</Button>
						<Button color="red" onClick={confirmDelete} loading={deleting}>
							Delete
						</Button>
					</Group>
				</Stack>
			</Modal>
		</Stack>
	);
}
