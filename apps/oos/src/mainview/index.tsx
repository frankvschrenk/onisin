// Mainview entry point — mounts the React App inside MantineProvider.
//
// Mantine's own CSS is imported here so the Electrobun bundler picks
// it up; index.css is reserved for top-level resets.
//
// Typography: IBM Plex Sans (loaded via @fontsource so the font
// ships with the bundle and works offline). Theme tokens, dark-mode
// CSS overrides and color-scheme manager all live in oos-theme-ts
// so the four OOS apps share one source of truth.
//
// ModalsProvider is wrapped around the whole app so any component
// can call modals.openConfirmModal() / modals.open() without
// thinking about scope. The detail tab uses it for the delete
// confirmation; future flows that need a contextual prompt can use
// the same handle.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MantineProvider }                  from "@mantine/core";
import { ModalsProvider }                   from "@mantine/modals";
import { Notifications }                    from "@mantine/notifications";
import { theme, colorSchemeManager }        from "oos-theme-ts";
import "@mantine/notifications/styles.css";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@mantine/core/styles.css";
import "@mdxeditor/editor/style.css";
import "oos-theme-ts/theme.css";

import { initMonaco } from "./monaco-init";

// Pin @monaco-editor/react to the bundled Monaco instance and
// set up the base worker before any editor mounts.
initMonaco();
import "./index.css";

import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");

createRoot(root).render(
	<StrictMode>
		<MantineProvider
			theme={theme}
			defaultColorScheme="auto"
			colorSchemeManager={colorSchemeManager}
		>
			<Notifications position="top-right" />
			<ModalsProvider>
				<App />
			</ModalsProvider>
		</MantineProvider>
	</StrictMode>,
);
