import type { Point } from '../../domain/types'
import { minMaxAvg } from '../../domain/series'

/** A gap wider than this between two samples of one series breaks the line (matches the STALE window). */
export const GAP_MS = 5 * 60_000

export interface AlignedData {
  /** Unix seconds (uPlot's default time unit), ascending. */
  xs: number[]
  /** One array per series, same length as xs; null where the series has no sample or a gap breaks the line. */
  ys: (number | null)[][]
}

/**
 * Aligns several series on one x axis, as uPlot requires. Where one series has a gap wider than GAP_MS
 * an extra x with null values is inserted so the line breaks instead of bridging the outage.
 */
export function alignSeries(series: Point[][], gapMs: number = GAP_MS): AlignedData {
  const times = new Set<number>()
  for (const s of series) for (const p of s) times.add(p.t)
  for (const s of series) {
    for (let i = 1; i < s.length; i++) {
      if (s[i]!.t - s[i - 1]!.t > gapMs) times.add(s[i - 1]!.t + 1000)
    }
  }
  const sorted = [...times].sort((a, b) => a - b)
  const ys = series.map((s) => {
    const byTime = new Map(s.map((p) => [p.t, p.v]))
    return sorted.map((t) => byTime.get(t) ?? null)
  })
  return { xs: sorted.map((t) => t / 1000), ys }
}

/** Number of real samples across all series (charts need at least two points to draw a line). */
export function sampleCount(series: Point[][]): number {
  return series.reduce((n, s) => Math.max(n, s.length), 0)
}

/** Y range: fixed for percentages; otherwise from zero with 10 % headroom, and never a zero-height axis. */
export function yRange(kind: 'percent' | 'auto', max: number): [number, number] {
  if (kind === 'percent') return [0, 100]
  if (!Number.isFinite(max) || max <= 0) return [0, 1]
  return [0, max * 1.1]
}

export interface Box {
  width: number
  height: number
}

/**
 * Places a tooltip next to the cursor and keeps it fully inside the container: to the right of the
 * cursor by default, flipped to the left when it would overflow, then clamped on both axes.
 */
export function clampTooltip(cursor: { left: number; top: number }, tip: Box, container: Box, offset = 12): { left: number; top: number } {
  let left = cursor.left + offset
  if (left + tip.width > container.width) left = cursor.left - offset - tip.width
  left = Math.max(0, Math.min(left, container.width - tip.width))
  let top = cursor.top + offset
  if (top + tip.height > container.height) top = cursor.top - offset - tip.height
  top = Math.max(0, Math.min(top, container.height - tip.height))
  return { left, top }
}

export interface SeriesSummary {
  latest: number | null
  min: number | null
  max: number | null
}

/** Text alternative of a chart: latest, min and max of each series over the window. */
export function summarize(points: Point[]): SeriesSummary {
  const stats = minMaxAvg(points)
  const last = points[points.length - 1]
  return { latest: last ? last.v : null, min: stats ? stats.min : null, max: stats ? stats.max : null }
}
