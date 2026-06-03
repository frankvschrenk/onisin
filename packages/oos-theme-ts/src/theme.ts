// theme.ts — shared Mantine theme tokens and component defaults.
//
// Single source of truth for visual styling across all OOS desktop
// apps (oos, oosd, ooso, bench, future). Components consume the
// tokens via CSS variables Mantine emits (var(--mantine-color-*),
// --mantine-radius-*, --mantine-font-family). Apps wire this into
// their MantineProvider:
//
//     import { theme } from "oos-theme-ts";
//     import "oos-theme-ts/theme.css";
//
//     <MantineProvider theme={theme} defaultColorScheme="auto"
//                      colorSchemeManager={colorSchemeManager}>
//
// The matching theme.css ships dark-mode CSS variable overrides:
// surface hierarchy, brand-scale swap, body text color. See that
// file for the dark-tuned brand scale and surface variables.
//
// Two named color scales:
//
//   brand  primary accent (deep indigo-blue). Mirrors the
//          onisin-home landing palette so app + marketing surface
//          read as one product family. Index 7 is the Light-mode
//          primary. Dark mode replaces this scale entirely via
//          theme.css (see comments there).
//
//   accent secondary accent (cyan). Used for highlights, selection
//          states, and inline links where a brand button would be
//          too heavy.
//
// Component defaults harmonise corner radii and density across
// Button / Card / Paper / Input / Modal so apps don't repeat the
// same `radius="md"` everywhere.

import { createTheme, type MantineColorsTuple } from "@mantine/core";

// Brand: deep indigo-blue, 10-stop tonal ramp tuned for LIGHT mode.
// Index 7 reads as primary on white surfaces. The scale below is
// dark-leaning by design (mid-tone is already dim) which makes it
// unusable in Dark mode — theme.css overrides --mantine-color-brand-*
// with a dark-tuned scale when the color scheme flips.
const brand: MantineColorsTuple = [
	"#e8edf8", // 0  lightest tint
	"#c5d0ef", // 1
	"#9fb2e4", // 2
	"#7893d9", // 3
	"#5574ce", // 4
	"#3b5bdb", // 5  bright brand (links, primary buttons)
	"#2d4ab8", // 6
	"#1e3a8a", // 7  Light-mode primary lands here
	"#172e6e", // 8
	"#0f2050", // 9  darkest
];

// Accent: cyan, balances the cool blue of brand with a warmer-bright
// note. Used sparingly: focus rings, selection backgrounds, badges.
const accent: MantineColorsTuple = [
	"#e3f7fb",
	"#c1eef5",
	"#94e1ec",
	"#66d3e2",
	"#3fc6d9",
	"#26b7cb",
	"#1c95a6",
	"#157482",
	"#0e555f",
	"#07363d",
];

export const theme = createTheme({
	primaryColor: "brand",
	colors: { brand, accent },

	// primaryShade picks WHICH stop of the brand scale is used as
	// "primary" per color scheme. Light mode reads best with a deep
	// stop (7) for strong contrast on white surfaces. Dark mode uses
	// stop 5 — in the dark-tuned brand scale (overridden via CSS in
	// theme.css) that stop is a luminous lavender-blue that stays
	// legible against the dark surfaces.
	primaryShade: { light: 7, dark: 5 },

	// IBM Plex Sans is loaded via @fontsource (peer of this package).
	// Chosen over Inter because Plex stays crisp on standard-density
	// external monitors (UWQHD/4K at ~110 ppi) where Inter softens.
	// The system mono stack matches what code editors / terminal
	// emulators use, so inline code reads continuous with editor
	// surfaces.
	fontFamily:
		'"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", ' +
		'Roboto, Oxygen, Ubuntu, Cantarell, "Open Sans", "Helvetica Neue", sans-serif',
	fontFamilyMonospace:
		'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, ' +
		'"Liberation Mono", monospace',

	// Slightly larger radius than Mantine's default (sm). Reads as
	// more contemporary without going full-pill.
	defaultRadius: "md",

	components: {
		Button:        { defaultProps: { radius: "md" } },
		ActionIcon:    { defaultProps: { radius: "md" } },
		Input:         { defaultProps: { radius: "md" } },
		TextInput:     { defaultProps: { radius: "md" } },
		Textarea:      { defaultProps: { radius: "md" } },
		Select:        { defaultProps: { radius: "md" } },
		NumberInput:   { defaultProps: { radius: "md" } },
		PasswordInput: { defaultProps: { radius: "md" } },
		Card:          { defaultProps: { radius: "md", withBorder: true } },
		Paper:         { defaultProps: { radius: "md" } },
		Modal:         { defaultProps: { radius: "md", centered: true } },
		Badge:         { defaultProps: { radius: "sm" } },
		Tabs: {
			// Default variant in Dark mode renders the indicator at
			// primary-shade-dark (brand.5) which reads clearly.
			// Bumping the indicator weight keeps it visible against
			// busy panels (forms, tables).
			styles: {
				tab: {
					borderBottomWidth: "2px",
				},
			},
		},
	},
});
