// src/mainview/index.tsx — React entrypoint.
//
// Theme tokens and color-scheme CSS live in oos-theme-ts so the OOS apps share
// one source of truth. MantineProvider itself lives in App.tsx because bench
// wraps the provider around its own shell (sidebar + content split).

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@mantine/core/styles.css";
import "oos-theme-ts/theme.css";
import "./index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const root = document.getElementById("root")!;
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
