import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'

const instances: FakeUPlot[] = []

class FakeUPlot {
  static pxRatio = 1
  setSize = vi.fn()
  setData = vi.fn()
  destroy = vi.fn()
  ctx = { font: '', measureText: (s: string) => ({ width: s.length * 7 }) }
  cursor: { idx: number | null; left: number; top: number } = { idx: null, left: -10, top: -10 }
  over = { offsetLeft: 40, offsetTop: 8 }
  data: unknown[] = []
  constructor(
    public opts: Record<string, unknown>,
    data: unknown[],
    public root: HTMLElement,
  ) {
    this.data = data
    instances.push(this)
  }
}

vi.mock('uplot', () => ({ default: FakeUPlot }))
vi.mock('uplot/dist/uPlot.min.css', () => ({}))

const { Chart } = await import('./Chart')

let resizeCallbacks: ((entries: { contentRect: { width: number } }[]) => void)[] = []

beforeEach(() => {
  instances.length = 0
  resizeCallbacks = []
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: (entries: { contentRect: { width: number } }[]) => void) {
        resizeCallbacks.push(cb)
      }
      observe() {}
      disconnect() {}
    },
  )
  vi.stubGlobal('matchMedia', () => ({ addEventListener() {}, removeEventListener() {} }))
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0))
  vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
})
afterEach(() => vi.unstubAllGlobals())

const pts = (n: number, base = 0) => Array.from({ length: n }, (_, i) => ({ t: 1_000_000 + i * 60_000, v: base + i }))
const fmt = (v: number) => `${v}%`

function resize(width: number) {
  act(() => resizeCallbacks.forEach((cb) => cb([{ contentRect: { width } }])))
}

describe('Chart', () => {
  it('draws nothing until it has a width and at least two samples', () => {
    const { rerender } = render(<Chart series={[{ label: 'CPU', points: pts(1) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    resize(200)
    expect(instances).toHaveLength(0)
    expect(screen.getByRole('status')).toHaveTextContent('—') // unknown stays visible
    rerender(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    expect(instances).toHaveLength(1)
  })

  it('does not draw while its container has zero width (collapsed accordion panel)', () => {
    render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    resize(0)
    expect(instances).toHaveLength(0)
    resize(180)
    expect(instances).toHaveLength(1)
    expect(instances[0]!.opts.width).toBe(180)
  })

  it('follows container resizes without recreating the chart', () => {
    render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="full" height={160} kind="percent" format={fmt} name="CPU" />)
    resize(300)
    resize(120)
    expect(instances).toHaveLength(1)
    expect(instances[0]!.setSize).toHaveBeenLastCalledWith({ width: 120, height: 160 })
  })

  it('resizes the canvas synchronously inside the observer callback, before React re-renders', () => {
    render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="full" height={160} kind="percent" format={fmt} name="CPU" />)
    resize(300)
    instances[0]!.setSize.mockClear()
    act(() => resizeCallbacks.forEach((cb) => cb([{ contentRect: { width: 90 } }])))
    expect(instances[0]!.setSize).toHaveBeenCalledWith({ width: 90, height: 160 })
  })

  it('destroys the chart on unmount', () => {
    const { unmount } = render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    resize(200)
    unmount()
    expect(instances[0]!.destroy).toHaveBeenCalledTimes(1)
  })

  it('turns off the built-in legend and fixes percent axes to 0-100', () => {
    render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="full" height={160} kind="percent" format={fmt} name="CPU" />)
    resize(300)
    const opts = instances[0]!.opts as { legend: { show: boolean }; scales: { y: { range: () => [number, number] } }; padding: number[] }
    expect(opts.legend.show).toBe(false)
    expect(opts.scales.y.range()).toEqual([0, 100])
    expect(opts.padding[1]).toBeGreaterThanOrEqual(16) // room for the last x tick label
  })

  it('measures the y axis from its real labels so they cannot be clipped', () => {
    render(<Chart series={[{ label: 'RX', points: pts(3) }]} variant="full" height={160} kind="auto" format={(v) => `${v} MB/s`} name="net" />)
    resize(300)
    const opts = instances[0]!.opts as { axes: { size?: (u: FakeUPlot, values: string[]) => number }[] }
    const size = opts.axes[1]!.size!
    const narrow = size(instances[0]!, ['1', '2'])
    const wide = size(instances[0]!, ['1.5 MB/s', '10.5 GB/s'])
    expect(wide).toBeGreaterThan(narrow)
    expect(wide).toBeGreaterThanOrEqual('10.5 GB/s'.length * 7)
  })

  it('pushes new data with setData on the next frame and keeps the same instance', async () => {
    const { rerender } = render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    resize(200)
    rerender(<Chart series={[{ label: 'CPU', points: pts(4) }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    expect(instances).toHaveLength(1)
    expect(instances[0]!.setData).toHaveBeenCalled()
  })

  it('keeps a measured zero (not null) in the data handed to uPlot', () => {
    render(<Chart series={[{ label: 'CPU', points: [{ t: 1000, v: 0 }, { t: 61_000, v: 0 }] }]} variant="spark" height={32} kind="percent" format={fmt} name="CPU" />)
    resize(200)
    expect(instances[0]!.data).toEqual([[1, 61], [0, 0]])
  })

  it('offers a text alternative: latest, min and max per series, and is accessible', async () => {
    const { container } = render(
      <Chart
        series={[{ label: 'load 1m', points: [{ t: 0, v: 0.5 }, { t: 60_000, v: 2 }] }, { label: 'load 5m', points: [] }]}
        variant="full"
        height={160}
        kind="auto"
        format={(v) => v.toFixed(2)}
        name="Load average, last hour"
      />,
    )
    expect(screen.getByRole('figure', { name: 'Load average, last hour' })).toBeInTheDocument()
    expect(screen.getByText(/2\.00 now · min 0\.50 · max 2\.00/)).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument() // the series without samples reads unknown
    expect(await axe(container)).toHaveNoViolations()
  })

  it('shows the tooltip inside the container when the cursor is near an edge', () => {
    render(<Chart series={[{ label: 'CPU', points: pts(3) }]} variant="full" height={160} kind="percent" format={fmt} name="CPU" />)
    resize(300)
    const u = instances[0]!
    const hook = ((u.opts.hooks as { setCursor: ((u: FakeUPlot) => void)[] }).setCursor)[0]!
    u.data = [[1, 2, 3], [10, 20, 30]]
    u.cursor = { idx: 2, left: 290, top: 150 }
    hook(u)
    const tip = document.querySelector<HTMLElement>('[aria-hidden="true"][hidden], div[class*="tip"]')
    expect(tip).not.toBeNull()
    const tipEl = document.querySelector<HTMLElement>('div[class*="tip"]')!
    expect(tipEl.hidden).toBe(false)
    expect(tipEl.textContent).toContain('CPU 30%')
    u.cursor = { idx: null, left: -10, top: -10 }
    hook(u)
    expect(tipEl.hidden).toBe(true)
  })
})
