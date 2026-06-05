// Mainview entry point — mounts the React App inside MantineProvider.
//
// Theme tokens and color-scheme live in oos-theme-ts so the OOS apps share one
// source of truth.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MantineProvider } from "@mantine/core";
import { theme } from "oos-theme-ts";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@mantine/core/styles.css";
import "oos-theme-ts/theme.css";
import "./index.css";

import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");

createRoot(root).render(
	<StrictMode>
		<MantineProvider theme={theme} forceColorScheme="light">
			<App />
		</MantineProvider>
	</StrictMode>,
);
