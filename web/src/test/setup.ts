import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import * as axeMatchers from 'vitest-axe/matchers'
import { afterEach, expect, vi } from 'vitest'

expect.extend(axeMatchers)

// jsdom has no canvas: charts are never really drawn in unit tests (real drawing is checked in a browser).
vi.mock('uplot', () => ({
  default: class {
    static pxRatio = 1
    ctx = { font: '', measureText: () => ({ width: 0 }) }
    setSize() {}
    setData() {}
    destroy() {}
  },
}))
vi.mock('uplot/dist/uPlot.min.css', () => ({}))

// jsdom lacks these browser APIs; components use them behind feature-neutral code paths.
class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
// Assigned directly (not vi.stubGlobal): tests call vi.unstubAllGlobals(), which must not remove these.
globalThis.ResizeObserver = NoopResizeObserver
window.matchMedia = (query: string) =>
  ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList

afterEach(() => {
  cleanup()
})
