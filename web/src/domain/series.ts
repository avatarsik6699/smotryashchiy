import type { MetricDTO, Point } from './types'

/** Sorted-by-time copy of the samples of one metric series. */
export function toPoints(samples: { ts: string; value: number }[]): Point[] {
  return samples.map((s) => ({ t: Date.parse(s.ts), v: s.value })).sort((a, b) => a.t - b.t)
}

/**
 * Rates of a monotonically increasing counter between consecutive samples. A decrease is a counter
 * reset (agent or host restart): it produces no point for that interval instead of a huge negative
 * or wrapped value. Non-advancing time is skipped too.
 */
export function counterRates(points: Point[]): Point[] {
  const out: Point[] = []
  for (let i = 1; i < points.length; i++) {
    const prev = points[i - 1]!
    const cur = points[i]!
    const dt = (cur.t - prev.t) / 1000
    const dv = cur.v - prev.v
    if (dt <= 0 || dv < 0) continue
    out.push({ t: cur.t, v: dv / dt })
  }
  return out
}

/** Sum of several rate series aligned on identical timestamps (interfaces reported in one batch). */
export function sumSeries(list: Point[][]): Point[] {
  const byTime = new Map<number, number>()
  for (const series of list) {
    for (const p of series) byTime.set(p.t, (byTime.get(p.t) ?? 0) + p.v)
  }
  return [...byTime.entries()].map(([t, v]) => ({ t, v })).sort((a, b) => a.t - b.t)
}

/** Groups metric samples by a label value (e.g. interface, mount) into per-label point series. */
export function groupByLabel(samples: MetricDTO[], label: string): Map<string, Point[]> {
  const grouped = new Map<string, MetricDTO[]>()
  for (const s of samples) {
    const key = s.labels[label] ?? ''
    const list = grouped.get(key)
    if (list) list.push(s)
    else grouped.set(key, [s])
  }
  return new Map([...grouped.entries()].map(([k, v]) => [k, toPoints(v)]))
}

/** The mount with the highest used_percent among the latest samples (the one worth watching). */
export function pickDiskMount(latestUsedPercent: MetricDTO[]): string | null {
  let best: MetricDTO | null = null
  for (const s of latestUsedPercent) {
    if (best === null || s.value > best.value) best = s
  }
  return best === null ? null : (best.labels.mount ?? null)
}

export function minMaxAvg(points: Point[]): { min: number; max: number; avg: number } | null {
  if (points.length === 0) return null
  let min = Infinity
  let max = -Infinity
  let sum = 0
  for (const p of points) {
    if (p.v < min) min = p.v
    if (p.v > max) max = p.v
    sum += p.v
  }
  return { min, max, avg: sum / points.length }
}
