import { isFresh } from '../domain/freshness'
import { counterRates, sumSeries } from '../domain/series'
import type { EventDTO, MetricDTO, Point } from '../domain/types'
import { seriesKey, type HostRecord } from './model'

/** Raw (10 s) metrics fetched for one expanded host over the last hour. */
export const DETAIL_METRICS = [
  'cpu.usage_percent',
  'memory.used_percent',
  'swap.used_percent',
  'load.avg_1m',
  'load.avg_5m',
  'load.avg_15m',
  'network.rx_bytes_total',
  'network.tx_bytes_total',
  'disk.used_percent',
] as const

/** Raw history followed by any live points newer than it (the store keeps extending its own tails). */
export function withLiveTail(raw: Point[], live: Point[] | undefined): Point[] {
  const last = raw[raw.length - 1]?.t ?? -Infinity
  const tail = (live ?? []).filter((p) => p.t > last)
  return tail.length === 0 ? raw : [...raw, ...tail]
}

function rawPoints(raw: MetricDTO[], record: HostRecord, name: string, labels: Record<string, string> = {}): Point[] {
  const mine = raw.filter((m) => m.name === name && seriesKey(name, m.labels) === seriesKey(name, labels))
  const points = mine.map((m) => ({ t: Date.parse(m.ts), v: m.value })).sort((a, b) => a.t - b.t)
  return withLiveTail(points, record.series.get(seriesKey(name, labels))?.points)
}

export interface InterfaceRate {
  name: string
  rx: number | null
  tx: number | null
}

export interface DetailSeries {
  cpu: Point[]
  memory: Point[]
  swap: Point[]
  load1: Point[]
  load5: Point[]
  load15: Point[]
  rx: Point[]
  tx: Point[]
  interfaces: InterfaceRate[]
}

function directionRates(raw: MetricDTO[], record: HostRecord, name: string): Map<string, Point[]> {
  const interfaces = new Set<string>()
  for (const m of raw) if (m.name === name) interfaces.add(m.labels.interface ?? '')
  for (const s of record.series.values()) if (s.name === name) interfaces.add(s.labels.interface ?? '')
  const out = new Map<string, Point[]>()
  for (const iface of interfaces) {
    const labels: Record<string, string> = iface === '' ? {} : { interface: iface }
    out.set(iface, counterRates(rawPoints(raw, record, name, labels)))
  }
  return out
}

export function buildDetailSeries(raw: MetricDTO[], record: HostRecord, nowMs: number): DetailSeries {
  const rx = directionRates(raw, record, 'network.rx_bytes_total')
  const tx = directionRates(raw, record, 'network.tx_bytes_total')
  const names = [...new Set([...rx.keys(), ...tx.keys()])].sort()
  const latest = (series: Map<string, Point[]>, iface: string): number | null => {
    const p = series.get(iface)?.at(-1)
    return p && isFresh(p.t, nowMs) ? p.v : null
  }
  return {
    cpu: rawPoints(raw, record, 'cpu.usage_percent'),
    memory: rawPoints(raw, record, 'memory.used_percent'),
    swap: rawPoints(raw, record, 'swap.used_percent'),
    load1: rawPoints(raw, record, 'load.avg_1m'),
    load5: rawPoints(raw, record, 'load.avg_5m'),
    load15: rawPoints(raw, record, 'load.avg_15m'),
    rx: sumSeries([...rx.values()]),
    tx: sumSeries([...tx.values()]),
    interfaces: names.map((name) => ({
      name,
      rx: latest(rx, name),
      tx: latest(tx, name),
    })),
  }
}

export interface DiskInfo {
  mount: string
  device: string
  usedPercent: number | null
  usedBytes: number | null
  totalBytes: number | null
}

/** Latest known state of every mounted disk; a value older than 5 minutes is unknown (null). */
export function diskInfos(record: HostRecord, nowMs: number): DiskInfo[] {
  const byMount = new Map<string, DiskInfo>()
  for (const s of record.series.values()) {
    if (!['disk.used_percent', 'disk.used_bytes', 'disk.total_bytes'].includes(s.name)) continue
    const mount = s.labels.mount
    if (mount === undefined) continue
    const info = byMount.get(mount) ?? {
      mount,
      device: s.labels.device ?? '',
      usedPercent: null,
      usedBytes: null,
      totalBytes: null,
    }
    const p = s.points.at(-1)
    const v = p && isFresh(p.t, nowMs) ? p.v : null
    if (s.name === 'disk.used_percent') info.usedPercent = v
    else if (s.name === 'disk.used_bytes') info.usedBytes = v
    else info.totalBytes = v
    byMount.set(mount, info)
  }
  return [...byMount.values()].sort((a, b) => a.mount.localeCompare(b.mount))
}

export interface ContainerInfo {
  name: string
  image: string
  cpuPercent: number | null
  memUsedBytes: number | null
  memUsedPercent: number | null
}

/**
 * Latest known state of every Docker container reporting `docker.container.*` metrics
 * (docs/SPEC.md §4h, §5). A host with no such metrics returns an empty list — the block is
 * omitted entirely rather than shown empty (Docker is optional, unlike disks).
 */
export function containerInfos(record: HostRecord, nowMs: number): ContainerInfo[] {
  const byName = new Map<string, ContainerInfo>()
  for (const s of record.series.values()) {
    if (!s.name.startsWith('docker.container.')) continue
    const name = s.labels.container
    if (name === undefined) continue
    const info = byName.get(name) ?? {
      name,
      image: s.labels.image ?? '',
      cpuPercent: null,
      memUsedBytes: null,
      memUsedPercent: null,
    }
    const p = s.points.at(-1)
    const v = p && isFresh(p.t, nowMs) ? p.v : null
    if (s.name === 'docker.container.cpu_percent') info.cpuPercent = v
    else if (s.name === 'docker.container.memory_used_bytes') info.memUsedBytes = v
    else if (s.name === 'docker.container.memory_used_percent') info.memUsedPercent = v
    byName.set(name, info)
  }
  return [...byName.values()].sort((a, b) => (b.cpuPercent ?? -1) - (a.cpuPercent ?? -1) || a.name.localeCompare(b.name))
}

/** Event list for one host: fetched ones plus newer live ones from the global stream, newest first, deduplicated. */
export function mergeHostEvents(fetched: EventDTO[], live: EventDTO[], hostId: string, limit = 20): EventDTO[] {
  const seen = new Set<string>()
  const out: EventDTO[] = []
  for (const e of [...live.filter((x) => x.host === hostId), ...fetched]) {
    const key = `${e.ts}|${e.level}|${e.message}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(e)
  }
  return out.sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts)).slice(0, limit)
}
