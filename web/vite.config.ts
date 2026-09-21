import { closeSync, openSync } from 'node:fs'
import { join } from 'node:path'
import react from '@vitejs/plugin-react'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitest/config'

// The Go binary embeds web/dist (go:embed all:dist), so the directory must always contain a file.
// Vite empties outDir before building; this puts the tracked .gitkeep back afterwards.
function keepDistPlaceholder(): Plugin {
  return {
    name: 'keep-dist-placeholder',
    apply: 'build',
    closeBundle() {
      closeSync(openSync(join(import.meta.dirname, 'dist', '.gitkeep'), 'a'))
    },
  }
}

const backend = process.env.SMOTRYASHCHIY_DEV_BACKEND ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react(), keepDistPlaceholder()],
  build: { outDir: 'dist', target: 'baseline-widely-available' },
  server: {
    // Same-origin in development too: the session cookie and the WebSocket Origin check both need it.
    proxy: { '/api': { target: backend, ws: true } },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    globals: true,
    css: { modules: { classNameStrategy: 'non-scoped' } },
  },
})
