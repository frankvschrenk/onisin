// ColorSchemeToggle.tsx — three-state header button cycling
// Light → Dark → Auto.
//
// The component is intentionally three-state instead of a binary
// switch: "Auto" is the meaningful default on a cross-platform
// desktop app, and a 2-way toggle silently hides that the user has
// taken control. Cycling makes the current mode explicit via icon
// + tooltip.
//
// The matching localStorageColorSchemeManager (exported from this
// package's index.ts) persists the choice across all OOS apps
// under a shared key so flipping in one app applies in the next.

import { ActionIcon, Tooltip, useMantineColorScheme } from "@mantine/core";
import { IconSun, IconMoon, IconDeviceDesktop } from "@tabler/icons-react";

export function ColorSchemeToggle() {
	const { colorScheme, setColorScheme } = useMantineColorScheme();

	const next = colorScheme === "light"
		? "dark"
		: colorScheme === "dark"
			? "auto"
			: "light";

	const icon = colorScheme === "light"
		? <IconSun size={18} />
		: colorScheme === "dark"
			? <IconMoon size={18} />
			: <IconDeviceDesktop size={18} />;

	const label = colorScheme === "light"
		? "Light — switch to Dark"
		: colorScheme === "dark"
			? "Dark — switch to Auto"
			: "Auto (OS) — switch to Light";

	return (
		<Tooltip label={label} withArrow openDelay={400}>
			<ActionIcon
				variant="default"
				size="lg"
				aria-label={label}
				onClick={() => setColorScheme(next)}
			>
				{icon}
			</ActionIcon>
		</Tooltip>
	);
}
