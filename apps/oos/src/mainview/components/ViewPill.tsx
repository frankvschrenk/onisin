// ViewPill.tsx — Visible chip below the chat input.
//
// Shows the view the resolver picked (or the user manually
// selected). Carries an X to remove the hint, in which case the
// agent falls back to its un-hinted Sandwich-prompt path. The
// chip also acts as the trigger for swapping the view: clicking
// the body opens the picker.
//
// Visual style: a Mantine Badge in indigo light, large enough to
// be obviously interactive. Sits in a flex row with no margin so
// the parent controls spacing.

import { Badge, CloseButton, Group } from "@mantine/core";
import { IconLayoutGrid } from "@tabler/icons-react";

interface ViewPillProps {
	viewName:  string;
	viewTitle: string;
	onClear:   () => void;
	onClick?:  () => void;
}

export function ViewPill({ viewName, viewTitle, onClear, onClick }: ViewPillProps) {
	return (
		<Group gap={6} align="center" wrap="nowrap">
			<Badge
				size="md"
				variant="light"
				color="brand"
				radius="sm"
				leftSection={<IconLayoutGrid size={12} />}
				style={{
					cursor:     onClick ? "pointer" : "default",
					textTransform: "none",
					paddingLeft:  8,
					paddingRight: 6,
				}}
				onClick={onClick}
				title={`View: ${viewName}`}
			>
				{viewTitle}
			</Badge>
			<CloseButton
				size="xs"
				aria-label="View-Hinweis entfernen"
				onClick={(e) => {
					e.stopPropagation();
					onClear();
				}}
			/>
		</Group>
	);
}
