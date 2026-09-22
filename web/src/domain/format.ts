// Formatting for a dense mono ledger: short, stable width, tabular. Unknown is always "—".

export const UNKNOWN = '—'

const pad2 = (n: number) => String(n).padStart(2, '0')

/** "12s ago", "3m ago", "5h ago", "2d ago"; a future timestamp (clock skew) reads "just now". */
export function formatAge(ageMs: number): string {
  if (ageMs < 5_000) return 'just now'
  const s = Math.floor(ageMs / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

/** "14d 06h", "3h 12m", "45m", "20s". */
export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return UNKNOWN
  const s = Math.floor(seconds)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${pad2(h)}h`
  if (h > 0) return `${h}h ${pad2(m)}m`
  if (m > 0) return `${m}m`
  return `${s}s`
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

/** Binary units with one decimal from KB up: "512 B", "1.5 KB", "20.0 GB". */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return UNKNOWN
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  return unit === 0 ? `${Math.round(value)} B` : `${value.toFixed(1)} ${UNITS[unit]}`
}

export function formatRate(bytesPerSecond: number): string {
  const text = formatBytes(bytesPerSecond)
  return text === UNKNOWN ? UNKNOWN : `${text}/s`
}

/** Whole percent, or one decimal below 10 % so small values stay distinguishable; 0 stays "0%". */
export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return UNKNOWN
  const v = Math.min(100, Math.max(0, value))
  return v < 10 && v > 0 ? `${v.toFixed(1)}%` : `${Math.round(v)}%`
}

/** "14:26:13" in the viewer's timezone. */
export function formatClock(ms: number): string {
  const d = new Date(ms)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

/** "12:30" for chart axes. */
export function formatAxisTime(ms: number): string {
  const d = new Date(ms)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

/** "12:30:15" for chart axes when the visible span is short enough that minutes would repeat. */
export function formatAxisTimeSeconds(ms: number): string {
  return `${formatAxisTime(ms)}:${pad2(new Date(ms).getSeconds())}`
}

/** "142 ms" or "1.3 s". */
export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return UNKNOWN
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`
}

/** "key=value, key2=value2" for a non-empty Check.meta object; "" when empty/absent (docs/SPEC.md §5). */
export function formatMeta(meta: unknown): string {
  if (typeof meta !== 'object' || meta === null || Array.isArray(meta)) return ''
  const entries = Object.entries(meta as Record<string, unknown>)
  return entries.map(([k, v]) => `${k}=${String(v)}`).join(', ')
}
