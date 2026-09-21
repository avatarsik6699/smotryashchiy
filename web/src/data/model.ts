import { isFresh } from '../domain/freshness'
import { counterRates, groupByLabel, pickDiskMount, sumSeries } from '../domain/series'
import type { CheckDTO, EventDTO, HostDTO, MetricDTO, Point } from '../domain/types'

/** Metrics that carry an hour of history for sparklines (docs/SPEC.md §5). */
export const HISTORY_METRICS = [
  'cpu.usage_percent',
  'memory.used_percent',
  'disk.used_percent',
  'network.rx_bytes_total',
  'network.tx_bytes_total',
] as const

export const WINDOW_MS = 60 * 60_000
export const MAX_EVENTS = 50

export interface Series {
  name: string
  labels: Record<string, string>
  /** Oldest first. Series that only need a current value hold a single point. */
  points: Point[]
}

export interface HostRecord {
  host: HostDTO
  /** Last time data was seen, ms; null = never. Server receipt time, advanced by live frames. */
  lastSeenMs: number | null
  series: Map<string, Series>
  checks: CheckDTO[]
}

export type ConnectionState = 'connecting' | 'live' | 'reconnecting' | 'offline'

export interface DashboardState {
  status: 'loading' | 'ready' | 'error'
  /** Message of the last failed (re)load; with status ready it means the data on screen is not refreshing. */
  error: string | null
  hosts: HostRecord[]
  events: EventDTO[]
  connection: ConnectionState
}

export const initialState: DashboardState = { status: 'loading', error: null, hosts: [], events: [], connection: 'connecting' }

export function seriesKey(name: string, labels: Record<string, string>): string {
  const parts = Object.keys(labels)
    .sort()
    .map((k) => `${k}=${labels[k]}`)
  return parts.length === 0 ? name : `${name}|${parts.join(',')}`
}

export interface LoadedData {
  hosts: HostDTO[]
  latest: MetricDTO[]
  history: MetricDTO[]
  events: EventDTO[]
  checks: CheckDTO[]
}

/** Builds the host records from one full load. Latest samples extend history so a value is never older than the newest sample. */
export function buildRecords(data: LoadedData): HostRecord[] {
  const byHost = new Map<string, HostRecord>()
  for (const host of data.hosts) {
    byHost.set(host.id, {
      host,
      lastSeenMs: host.last_seen_at === null ? null : Date.parse(host.last_seen_at),
      series: new Map(),
      checks: [],
    })
  }
  const add = (m: MetricDTO) => {
    const rec = byHost.get(m.host)
    if (!rec) return
    const key = seriesKey(m.name, m.labels)
    const t = Date.parse(m.ts)
    const existing = rec.series.get(key)
    if (!existing) {
      rec.series.set(key, { name: m.name, labels: m.labels, points: [{ t, v: m.value }] })
      return
    }
    const last = existing.points[existing.points.length - 1]
    if (!last || t > last.t) existing.points.push({ t, v: m.value })
    else if (t < last.t) {
      existing.points.push({ t, v: m.value })
      existing.points.sort((a, b) => a.t - b.t)
    }
  }
  // History arrives sorted by time from the server; latest then tops it up.
  for (const m of [...data.history].sort((a, b) => Date.parse(a.ts) - Date.parse(b.ts))) add(m)
  for (const m of data.latest) {
    const rec = byHost.get(m.host)
    const s = rec?.series.get(seriesKey(m.name, m.labels))
    if (s && s.points.some((p) => p.t === Date.parse(m.ts))) continue
    add(m)
  }
  for (const c of data.checks) byHost.get(c.host)?.checks.push(c)
  return [...byHost.values()].sort((a, b) => a.host.name.localeCompare(b.host.name) || a.host.id.localeCompare(b.host.id))
}

export interface StreamFrame {
  type: 'metric' | 'check' | 'event'
  host_id: string
  record: MetricDTO | CheckDTO | EventDTO
}

/**
 * Applies one live frame. Returns the same state object when nothing changed and `unknownHost` when
 * the frame belongs to a host that is not loaded yet (the caller refreshes the host list).
 */
export function applyFrame(state: DashboardState, frame: StreamFrame, nowMs: number): { state: DashboardState; unknownHost: boolean } {
  if (frame.type === 'event') {
    const event = frame.record as EventDTO
    return { state: { ...state, events: [event, ...state.events].slice(0, MAX_EVENTS) }, unknownHost: false }
  }
  const index = state.hosts.findIndex((h) => h.host.id === frame.host_id)
  if (index < 0) return { state, unknownHost: true }
  const rec = state.hosts[index]!
  let next: HostRecord
  if (frame.type === 'metric') {
    const m = frame.record as MetricDTO
    const key = seriesKey(m.name, m.labels)
    const t = Date.parse(m.ts)
    const existing = rec.series.get(key)
    const cutoff = nowMs - WINDOW_MS
    const points = existing ? existing.points.filter((p) => p.t >= cutoff) : []
    const last = points[points.length - 1]
    if (last && t <= last.t) return { state, unknownHost: false } // replay or out-of-order: ignore
    points.push({ t, v: m.value })
    const series = new Map(rec.series)
    series.set(key, { name: m.name, labels: m.labels, points })
    next = { ...rec, series, lastSeenMs: Math.max(rec.lastSeenMs ?? 0, nowMs) }
  } else {
    const c = frame.record as CheckDTO
    next = { ...rec, checks: [...rec.checks.filter((x) => x.name !== c.name), c].sort((a, b) => a.name.localeCompare(b.name)), lastSeenMs: Math.max(rec.lastSeenMs ?? 0, nowMs) }
  }
  const hosts = state.hosts.slice()
  hosts[index] = next
  return { state: { ...state, hosts }, unknownHost: false }
}

// ---- selectors -------------------------------------------------------------------------------

function lastPoint(rec: HostRecord, name: string, labels: Record<string, string> = {}): Point | null {
  const s = rec.series.get(seriesKey(name, labels))
  return s ? (s.points[s.points.length - 1] ?? null) : null
}

/** Current value of a series, or null when it has no sample or the sample is older than 5 minutes. */
export function currentValue(rec: HostRecord, name: string, nowMs: number, labels: Record<string, string> = {}): number | null {
  const p = lastPoint(rec, name, labels)
  return p && isFresh(p.t, nowMs) ? p.v : null
}

function seriesOf(rec: HostRecord, name: string): MetricDTO[] {
  const out: MetricDTO[] = []
  for (const s of rec.series.values()) {
    if (s.name !== name) continue
    for (const p of s.points) out.push({ host: rec.host.id, name, ts: new Date(p.t).toISOString(), value: p.v, labels: s.labels })
  }
  return out
}

export interface HostView {
  id: string
  name: string
  lastSeenMs: number | null
  uptimeSeconds: number | null
  cpu: { value: number | null; points: Point[] }
  memory: { value: number | null; points: Point[] }
  disk: { mount: string | null; value: number | null; points: Point[] }
  network: { rate: number | null; points: Point[] }
}

/** Network throughput: rx+tx bytes/s summed over interfaces, from counter deltas. */
export function networkPoints(rec: HostRecord): Point[] {
  const rates: Point[][] = []
  for (const name of ['network.rx_bytes_total', 'network.tx_bytes_total']) {
    for (const [, points] of groupByLabel(seriesOf(rec, name), 'interface')) rates.push(counterRates(points))
  }
  return sumSeries(rates)
}

export function hostView(rec: HostRecord, nowMs: number): HostView {
  const latestDisks: MetricDTO[] = []
  for (const s of rec.series.values()) {
    if (s.name !== 'disk.used_percent') continue
    const p = s.points[s.points.length - 1]
    if (p && isFresh(p.t, nowMs)) latestDisks.push({ host: rec.host.id, name: s.name, ts: new Date(p.t).toISOString(), value: p.v, labels: s.labels })
  }
  const mount = pickDiskMount(latestDisks)
  const diskSeries = mount === null ? undefined : [...rec.series.values()].find((s) => s.name === 'disk.used_percent' && s.labels.mount === mount)
  const net = networkPoints(rec)
  const lastNet = net[net.length - 1]
  return {
    id: rec.host.id,
    name: rec.host.name,
    lastSeenMs: rec.lastSeenMs,
    uptimeSeconds: currentValue(rec, 'uptime.seconds', nowMs),
    cpu: { value: currentValue(rec, 'cpu.usage_percent', nowMs), points: rec.series.get('cpu.usage_percent')?.points ?? [] },
    memory: { value: currentValue(rec, 'memory.used_percent', nowMs), points: rec.series.get('memory.used_percent')?.points ?? [] },
    disk: { mount, value: mount === null ? null : (latestDisks.find((d) => d.labels.mount === mount)?.value ?? null), points: diskSeries?.points ?? [] },
    network: { rate: lastNet && isFresh(lastNet.t, nowMs) ? lastNet.v : null, points: net },
  }
}

