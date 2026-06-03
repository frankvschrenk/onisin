// index.ts — public surface of oos-theme-ts.
//
// Apps wire the theme like this:
//
//     import {
//         theme,
//         colorSchemeManager,
//         ColorSchemeToggle,
//     } from "oos-theme-ts";
//     import "oos-theme-ts/theme.css";
//     import "@fontsource/ibm-plex-sans/400.css";
//     import "@fontsource/ibm-plex-sans/500.css";
//     import "@fontsource/ibm-plex-sans/600.css";
//     import "@fontsource/ibm-plex-sans/700.css";
//
//     <MantineProvider
//         theme={theme}
//         defaultColorScheme="auto"
//         colorSchemeManager={colorSchemeManager}
//     >
//         ...
//         <ColorSchemeToggle />
//     </MantineProvider>
//
// The font @import lines are intentionally left to the app rather
// than re-exported here — Electrobun's bundler resolves CSS imports
// from the app's own module graph, not transitive deps.

import { localStorageColorSchemeManager } from "@mantine/core";

export { theme }              from "./theme.js";
export { ColorSchemeToggle }  from "./ColorSchemeToggle.js";

// One shared key across all OOS apps. Toggling color scheme in one
// app applies to the others on next open — the Onisin suite reads
// as one product with one setting, not four separate apps with
// independent state.
export const colorSchemeManager = localStorageColorSchemeManager({
	key: "onisin-color-scheme",
});
