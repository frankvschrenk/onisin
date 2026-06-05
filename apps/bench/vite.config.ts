import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Tauri-oriented Vite config (same shape as oosd). Fixed dev port that
// tauri.conf.json's devUrl points at; src-tauri is excluded from the watcher
// so Rust rebuilds don't bounce the frontend.
const host = process.env.TAURI_DEV_HOST

export default defineConfig({
  plugins: [react()],
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
