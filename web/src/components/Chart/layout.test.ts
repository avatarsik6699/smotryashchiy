import { describe, expect, it } from 'vitest'
import { alignSeries, clampTooltip, GAP_MS, sampleCount, summarize, yRange } from './layout'

const p = (t: number, v: number) => ({ t, v })

describe('alignSeries', () => {
  it('unions timestamps, converts to seconds and pads missing samples with null', () => {
    const { xs, ys } = alignSeries([[p(1000, 1), p(3000, 3)], [p(2000, 20), p(3000, 30)]])
    expect(xs).toEqual([1, 2, 3])
    expect(ys).toEqual([[1, null, 3], [null, 20, 30]])
  })

  it('keeps a measured zero as 0, not null', () => {
    const { ys } = alignSeries([[p(1000, 0), p(2000, 0)]])
    expect(ys[0]).toEqual([0, 0])
  })

  it('breaks the line across an outage longer than the gap window', () => {
    const { xs, ys } = alignSeries([[p(0, 1), p(GAP_MS + 60_000, 2)]])
    expect(xs).toHaveLength(3) // an extra x just after the first sample carries the break
    expect(ys[0]).toEqual([1, null, 2])
    expect(xs[1]).toBe(1)
  })

  it('does not break the line for normal spacing', () => {
    const { xs } = alignSeries([[p(0, 1), p(60_000, 2), p(120_000, 3)]])
    expect(xs).toEqual([0, 60, 120])
  })

  it('handles empty input', () => {
    expect(alignSeries([])).toEqual({ xs: [], ys: [] })
    expect(alignSeries([[]])).toEqual({ xs: [], ys: [[]] })
  })
})

describe('sampleCount / yRange', () => {
  it('counts the longest series', () => {
    expect(sampleCount([[p(1, 1)], [p(1, 1), p(2, 2)]])).toBe(2)
    expect(sampleCount([])).toBe(0)
  })
  it('fixes percentages to 0..100 and never returns a zero-height axis', () => {
    expect(yRange('percent', 12)).toEqual([0, 100])
    expect(yRange('auto', 0)).toEqual([0, 1])
    expect(yRange('auto', NaN)).toEqual([0, 1])
    expect(yRange('auto', 100)[1]).toBeCloseTo(110)
  })
})

describe('clampTooltip', () => {
  const container = { width: 400, height: 200 }
  const tip = { width: 120, height: 40 }

  it('sits right and below the cursor when there is room', () => {
    expect(clampTooltip({ left: 50, top: 50 }, tip, container)).toEqual({ left: 62, top: 62 })
  })
  it('flips to the left near the right edge and never leaves the container', () => {
    const pos = clampTooltip({ left: 390, top: 50 }, tip, container)
    expect(pos.left).toBe(390 - 12 - 120)
    expect(pos.left + tip.width).toBeLessThanOrEqual(container.width)
  })
  it('flips above near the bottom edge', () => {
    const pos = clampTooltip({ left: 50, top: 190 }, tip, container)
    expect(pos.top).toBe(190 - 12 - 40)
  })
  it.each([
    [{ left: 0, top: 0 }],
    [{ left: 400, top: 200 }],
    [{ left: -50, top: -50 }],
    [{ left: 999, top: 999 }],
    [{ left: 200, top: 100 }],
  ])('always stays inside for cursor %j', (cursor) => {
    const pos = clampTooltip(cursor, tip, container)
    expect(pos.left).toBeGreaterThanOrEqual(0)
    expect(pos.top).toBeGreaterThanOrEqual(0)
    expect(pos.left + tip.width).toBeLessThanOrEqual(container.width)
    expect(pos.top + tip.height).toBeLessThanOrEqual(container.height)
  })
  it('pins to the origin when the tooltip is larger than the container', () => {
    expect(clampTooltip({ left: 10, top: 10 }, { width: 500, height: 300 }, container)).toEqual({ left: 0, top: 0 })
  })
})

describe('summarize', () => {
  it('reports latest, min and max, including zero', () => {
    expect(summarize([p(1, 5), p(2, 0), p(3, 3)])).toEqual({ latest: 3, min: 0, max: 5 })
  })
  it('is all null for no data', () => {
    expect(summarize([])).toEqual({ latest: null, min: null, max: null })
  })
})
