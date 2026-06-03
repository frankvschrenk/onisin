import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Tauri-oriented Vite config (crypto pattern). The dev server runs on a
// fixed port that tauri.conf.json points devUrl at; src-tauri is ignored
// by the watcher so Rust rebuilds don't trigger a frontend reload.
const host = process.env.TAURI_DEV_HOST

export default defineConfig({
  plugins: [react()],
  // Monaco ships its language workers as separate assets; let Vite carry
  // .wasm through untouched (some Monaco features pull wasm in).
  assetsInclude: ['**/*.wasm'],

  // Don't wipe Tauri's CLI output when Vite logs.
  clearScreen: false,

  build: {
    // Desktop app: a large single bundle is fine, silence the warning.
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
