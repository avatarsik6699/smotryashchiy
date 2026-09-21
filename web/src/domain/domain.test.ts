import { describe, expect, it } from 'vitest'
import { HOST_STATE_LABEL, hostState, isFresh, OK_WINDOW_MS, STALE_WINDOW_MS } from './freshness'
import { formatAge, formatBytes, formatPercent, formatRate, formatUptime, UNKNOWN } from './format'
import { counterRates, groupByLabel, minMaxAvg, pickDiskMount, sumSeries, toPoints } from './series'
import type { MetricDTO } from './types'

const NOW = Date.UTC(2026, 8, 21, 12, 0, 0)

describe('hostState', () => {
  it('is NEW when the host was never seen, never a date', () => {
    expect(hostState(null, NOW)).toBe('new')
  })
  it('follows the SPEC thresholds at the boundaries', () => {
    expect(hostState(NOW - OK_WINDOW_MS, NOW)).toBe('ok')
    expect(hostState(NOW - OK_WINDOW_MS - 1, NOW)).toBe('stale')
    expect(hostState(NOW - STALE_WINDOW_MS, NOW)).toBe('stale')
    expect(hostState(NOW - STALE_WINDOW_MS - 1, NOW)).toBe('offline')
  })
  it('treats a sample slightly in the future (clock skew) as fresh', () => {
    expect(hostState(NOW + 3000, NOW)).toBe('ok')
  })
  it('has text for every state', () => {
    expect(Object.values(HOST_STATE_LABEL)).toEqual(['OK', 'STALE', 'OFFLINE', 'NEW'])
  })
})

describe('isFresh', () => {
  it('trusts a value for 5 minutes only', () => {
    expect(isFresh(NOW - 299_000, NOW)).toBe(true)
    expect(isFresh(NOW - 301_000, NOW)).toBe(false)
  })
})

describe('formatting', () => {
  it('formats ages', () => {
    expect(formatAge(-1000)).toBe('just now')
    expect(formatAge(4000)).toBe('just now')
    expect(formatAge(12_000)).toBe('12s ago')
    expect(formatAge(3 * 60_000)).toBe('3m ago')
    expect(formatAge(5 * 3600_000)).toBe('5h ago')
    expect(formatAge(3 * 86400_000)).toBe('3d ago')
  })
  it('formats uptime', () => {
    expect(formatUptime(20)).toBe('20s')
    expect(formatUptime(45 * 60)).toBe('45m')
    expect(formatUptime(3 * 3600 + 12 * 60)).toBe('3h 12m')
    expect(formatUptime(14 * 86400 + 6 * 3600 + 22 * 60)).toBe('14d 06h')
    expect(formatUptime(-1)).toBe(UNKNOWN)
    expect(formatUptime(NaN)).toBe(UNKNOWN)
  })
  it('formats bytes and rates with binary units', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(20 * 1024 ** 3)).toBe('20.0 GB')
    expect(formatBytes(-1)).toBe(UNKNOWN)
    expect(formatRate(2 * 1024 ** 2)).toBe('2.0 MB/s')
    expect(formatRate(Infinity)).toBe(UNKNOWN)
  })
  it('formats percents: zero stays 0%, small values keep a decimal, clamps to 0..100', () => {
    expect(formatPercent(0)).toBe('0%')
    expect(formatPercent(0.04)).toBe('0.0%')
    expect(formatPercent(3.26)).toBe('3.3%')
    expect(formatPercent(37.4)).toBe('37%')
    expect(formatPercent(140)).toBe('100%')
    expect(formatPercent(-3)).toBe('0%')
    expect(formatPercent(NaN)).toBe(UNKNOWN)
  })
})

describe('counterRates', () => {
  const p = (t: number, v: number) => ({ t: t * 1000, v })
  it('turns counter deltas into per-second rates', () => {
    expect(counterRates([p(0, 100), p(10, 300), p(20, 300)])).toEqual([
      { t: 10_000, v: 20 },
      { t: 20_000, v: 0 }, // an idle interval is a measured zero, not a gap
    ])
  })
  it('skips a counter reset instead of emitting a negative or huge value', () => {
    expect(counterRates([p(0, 1000), p(10, 2000), p(20, 50), p(30, 150)])).toEqual([
      { t: 10_000, v: 100 },
      { t: 30_000, v: 10 },
    ])
  })
  it('skips samples that do not advance in time and handles tiny inputs', () => {
    expect(counterRates([p(0, 1), p(0, 5)])).toEqual([])
    expect(counterRates([p(0, 1)])).toEqual([])
    expect(counterRates([])).toEqual([])
  })
})

describe('series helpers', () => {
  it('sums aligned series and sorts by time', () => {
    const a = [{ t: 2, v: 1 }, { t: 1, v: 1 }]
    const b = [{ t: 1, v: 2 }, { t: 3, v: 5 }]
    expect(sumSeries([a, b])).toEqual([{ t: 1, v: 3 }, { t: 2, v: 1 }, { t: 3, v: 5 }])
  })
  it('sorts points parsed from the API', () => {
    const pts = toPoints([{ ts: '2026-09-21T12:00:10Z', value: 2 }, { ts: '2026-09-21T12:00:00Z', value: 1 }])
    expect(pts.map((x) => x.v)).toEqual([1, 2])
  })
  it('groups by label and keeps missing labels under an empty key', () => {
    const m = (labels: Record<string, string>, value: number, ts: string): MetricDTO => ({ host: 'h', name: 'n', ts, value, labels })
    const grouped = groupByLabel([m({ interface: 'eth0' }, 1, '2026-09-21T12:00:00Z'), m({ interface: 'eth1' }, 2, '2026-09-21T12:00:00Z'), m({}, 3, '2026-09-21T12:00:00Z')], 'interface')
    expect([...grouped.keys()].sort()).toEqual(['', 'eth0', 'eth1'])
  })
  it('picks the fullest disk mount, or null when there are no disks', () => {
    const d = (mount: string, value: number): MetricDTO => ({ host: 'h', name: 'disk.used_percent', ts: '2026-09-21T12:00:00Z', value, labels: { mount } })
    expect(pickDiskMount([d('/', 41), d('/data', 88), d('/boot', 12)])).toBe('/data')
    expect(pickDiskMount([])).toBeNull()
  })
  it('summarizes min/max/avg and returns null for no data', () => {
    expect(minMaxAvg([{ t: 1, v: 0 }, { t: 2, v: 10 }])).toEqual({ min: 0, max: 10, avg: 5 })
    expect(minMaxAvg([])).toBeNull()
  })
})
