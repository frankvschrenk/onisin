// Mainview entry point — mounts React App inside MantineProvider.
//
// Theme tokens, dark-mode CSS overrides and color-scheme manager all
// live in oos-theme-ts so the OOS apps share one source of truth.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MantineProvider }                  from "@mantine/core";
import { theme }                            from "oos-theme-ts";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@mantine/core/styles.css";
import "@mantine/dates/styles.css";
import "oos-theme-ts/theme.css";
import "./index.css";

import { App }        from "./App";
import { initMonaco } from "../lang/monaco/init";

// Pin @monaco-editor/react to the bundled Monaco instance before any
// editor mounts; otherwise the loader fetches a separate Monaco from
// a CDN and our language/marker registrations land on the wrong one.
initMonaco();

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");

createRoot(root).render(
	<StrictMode>
		<MantineProvider theme={theme} forceColorScheme="light">
			<App />
		</MantineProvider>
	</StrictMode>,
);
