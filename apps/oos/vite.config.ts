import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Tauri-oriented Vite config (mirrors apps/oosd). The dev server runs on a
// fixed port that tauri.conf.json points devUrl at; src-tauri is ignored by
// the watcher so Rust rebuilds don't trigger a frontend reload. Monaco and
// the Langium diagnostics worker are imported via `new Worker(new URL(...))`
// in code, so no worker plugin is needed here \u2014 only .wasm passthrough
// for Monaco features and the in-webview ONNX embedder.
const host = process.env.TAURI_DEV_HOST

export default defineConfig({
  plugins: [react()],
  assetsInclude: ['**/*.wasm'],
  clearScreen: false,
  build: {
    chunkSizeWarningLimit: 3000,
  },
  server: {
    port: 5173,
    strictPort: true,
    host: host || false,
    hmr: host ? { protocol: 'ws', host, port: 1421 } : undefined,
    watch: {
      ignored: ['**/src-tauri/**'],
    },
  },
})
