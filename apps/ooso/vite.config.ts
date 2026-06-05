import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Tauri-oriented Vite config (oosd/crypto pattern). Fixed dev port that
// tauri.conf.json's devUrl points at; 5174 so it does not collide with oosd's
// 5173 when both run under mprocs. src-tauri is ignored by the watcher.
const host = process.env.TAURI_DEV_HOST

export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  build: {
    chunkSizeWarningLimit: 3000,
  },
  server: {
    port: 5174,
    strictPort: true,
    host: host || false,
    hmr: host ? { protocol: 'ws', host, port: 1422 } : undefined,
    watch: {
      ignored: ['**/src-tauri/**'],
    },
  },
})
