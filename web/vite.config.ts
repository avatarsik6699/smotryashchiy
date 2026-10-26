import { closeSync, openSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { extname, join } from 'node:path'
import { brotliCompressSync, constants, gzipSync } from 'node:zlib'
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

// Change 38 (docs/SPEC.md §4d): every text file of the build ships with .br and .gz variants that
// the Go server picks by Accept-Encoding (web/embed.go), so nothing is compressed per request.
function precompress(): Plugin {
  const textTypes = new Set(['.js', '.css', '.html', '.svg', '.json', '.webmanifest'])
  return {
    name: 'precompress',
    apply: 'build',
    closeBundle() {
      const walk = (dir: string): string[] =>
        readdirSync(dir).flatMap((name) => {
          const p = join(dir, name)
          return statSync(p).isDirectory() ? walk(p) : [p]
        })
      for (const file of walk(join(import.meta.dirname, 'dist'))) {
        if (!textTypes.has(extname(file))) continue
        const data = readFileSync(file)
        if (data.length < 1024) continue
        writeFileSync(`${file}.br`, brotliCompressSync(data, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: data.length } }))
        writeFileSync(`${file}.gz`, gzipSync(data, { level: 9 }))
      }
    },
  }
}

const backend = process.env.SMOTRYASHCHIY_DEV_BACKEND ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react(), keepDistPlaceholder(), precompress()],
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
