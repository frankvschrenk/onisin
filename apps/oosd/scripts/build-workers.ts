// scripts/build-workers.ts
//
// Bundle the worker(s) the renderer needs at runtime:
//
//   - diagnostics-worker.js  — our Langium-driven validator. Used
//                              by both the mainview editor (for
//                              live diagnostics) and the detached
//                              preview window (for parsing the
//                              source it receives from the bun
//                              process). It is therefore copied
//                              into both view directories.
//   - editor.worker.js       — Monaco's own base worker; without
//                              it Monaco falls back to running the
//                              editor pipeline on the main thread
//                              and refuses to set up tokenization,
//                              which kills our Monarch highlighter
//                              and language registrations. Only
//                              the mainview hosts Monaco, so this
//                              one ships only there.
//
// Files land directly in src/<view>/workers/ (no nested
// directories), so the URL strings in monaco-init.ts and
// worker.ts stay short and obvious. The relative URL
// "workers/diagnostics-worker.js" resolves correctly from each
// view's index.html.
//
// We use esbuild rather than Bun.build because Bun's bundler
// mishandles certain CJS namespace imports — specifically the
// vscode-jsonrpc cancellation module that Langium pulls in
// transitively. Bun emits references to `exports_cancellation`
// without ever defining the variable, which manifests at runtime
// as `Can't find variable: exports_cancellation`. esbuild handles
// the same shape correctly.
//
// Output paths match the `copy:` entries in electrobun.config.ts —
// the bundler writes here, Electrobun copies into views/<view>/.../
// during its own build.
//
// Run with:  bun run scripts/build-workers.ts
//
// Wired into npm `predev` and `prebuild` so it runs automatically.

import { rmSync, mkdirSync, copyFileSync } from "node:fs";
import { createRequire } from "node:module";
import * as esbuild from "esbuild";

const require = createRequire(import.meta.url);

const MAIN_DIR    = "src/mainview/workers";
const PREVIEW_DIR = "src/previewview/workers";

for (const dir of [MAIN_DIR, PREVIEW_DIR]) {
	rmSync(dir, { recursive: true, force: true });
	mkdirSync(dir, { recursive: true });
}

type Entry = { entrypoint: string; outfile: string };

const entries: Entry[] = [
	{
		entrypoint: "src/lang/worker/diagnostics-worker.ts",
		outfile: `${MAIN_DIR}/diagnostics-worker.js`,
	},
	{
		// Monaco ships its base worker as a real ES module; we just
		// bundle it as-is so it can be loaded as a web worker.
		// Resolved via Node's module resolution so it works whether
		// monaco-editor is hoisted to the workspace root or sits in
		// oosd's own node_modules.
		entrypoint: require.resolve("monaco-editor/esm/vs/editor/editor.worker.js"),
		outfile: `${MAIN_DIR}/editor.worker.js`,
	},
];

for (const e of entries) {
	await esbuild.build({
		entryPoints: [e.entrypoint],
		outfile: e.outfile,
		bundle: true,
		platform: "browser",
		format: "iife",
		target: "es2022",
		sourcemap: "linked",
		// Workers loaded via `new Worker(url)` — no module loader on
		// the other side, so emit a self-contained IIFE.
		logLevel: "info",
	});
	console.log(`built ${e.outfile}`);
}

// Mirror the diagnostics worker into the previewview's worker
// directory. Copying is cheaper than a second esbuild pass and
// guarantees byte-for-byte parity with the mainview version, so
// any divergence is impossible by construction.
copyFileSync(
	`${MAIN_DIR}/diagnostics-worker.js`,
	`${PREVIEW_DIR}/diagnostics-worker.js`,
);
copyFileSync(
	`${MAIN_DIR}/diagnostics-worker.js.map`,
	`${PREVIEW_DIR}/diagnostics-worker.js.map`,
);
console.log(`copied diagnostics-worker.js → ${PREVIEW_DIR}/`);
