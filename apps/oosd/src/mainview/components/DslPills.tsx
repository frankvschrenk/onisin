// DslPills.tsx — Resolved domain and view chips below the chat input.
//
// Shows what the MiniLM resolver picked from the user's typed text.
// Domain pill: green — shows which domain source will be injected.
// View pill: indigo — shows where the result will be saved; click
//            opens that view in the Designer editor.
// Both carry an X to clear the hint independently.
//
// Moved here from the retired chatview/ window: the chat is now an
// inline mainview panel, so its pills live beside it.

import { Badge, CloseButton, Group, Text } from "@mantine/core";
import { IconDatabase, IconLayoutGrid } from "@tabler/icons-react";

interface DslPillsProps {
	domainIds: string[];
	viewId?:   string;
	onClearDomain: (id: string) => void;
	onClearView:   () => void;
	onClickView?:  () => void;
}

export function DslPills({
	domainIds,
	viewId,
	onClearDomain,
	onClearView,
	onClickView,
}: DslPillsProps) {
	if (domainIds.length === 0 && !viewId) return null;

	return (
		<Group gap={6} px="sm" pb={4} wrap="wrap">
			<Text size="xs" c="dimmed" style={{ lineHeight: "24px" }}>
				Erkannt:
			</Text>

			{domainIds.map((id) => (
				<Group key={id} gap={4} align="center" wrap="nowrap">
					<Badge
						size="md"
						variant="light"
						color="green"
						radius="sm"
						leftSection={<IconDatabase size={12} />}
						style={{ textTransform: "none", paddingLeft: 8, paddingRight: 6 }}
						title={`Domain: ${id}`}
					>
						{id}
					</Badge>
					<CloseButton
						size="xs"
						aria-label="Domain-Hinweis entfernen"
						onClick={() => onClearDomain(id)}
					/>
				</Group>
			))}

			{viewId && (
				<Group gap={4} align="center" wrap="nowrap">
					<Badge
						size="md"
						variant="light"
						color="indigo"
						radius="sm"
						leftSection={<IconLayoutGrid size={12} />}
						style={{
							textTransform: "none",
							paddingLeft:  8,
							paddingRight: 6,
							cursor: onClickView ? "pointer" : "default",
						}}
						onClick={onClickView}
						title={`View: ${viewId}`}
					>
						{viewId}
					</Badge>
					<CloseButton
						size="xs"
						aria-label="View-Hinweis entfernen"
						onClick={(e) => { e.stopPropagation(); onClearView(); }}
					/>
				</Group>
			)}
		</Group>
	);
}
