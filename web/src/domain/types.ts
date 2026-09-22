// Wire types of the JSON API (docs/SPEC.md §4.3, §4d). Field names are exactly the server's.

export interface HostDTO {
  id: string
  name: string
  created_at: string
  /** null until the first stored batch: "never seen" is a state, not a date. */
  last_seen_at: string | null
}

export interface MetricDTO {
  host: string
  name: string
  ts: string
  value: number
  labels: Record<string, string>
}

export interface CheckDTO {
  host: string
  name: string
  ts: string
  status: 'ok' | 'warn' | 'critical'
  meta: unknown
}

export type EventLevel = 'info' | 'warn' | 'error' | 'critical'

export interface EventDTO {
  host: string
  ts: string
  level: EventLevel
  message: string
  labels: Record<string, string>
}

/** A time-value pair; t is unix milliseconds. */
export interface Point {
  t: number
  v: number
}

export type UptimeKind = 'http' | 'tcp' | 'tls'

export interface UptimeResultDTO {
  target_id: string
  ts: string
  ok: boolean
  /** null when the target did not answer: no measurement is not 0 ms. */
  latency_ms: number | null
  status_code: number | null
  error: string
  cert_expires_at: string | null
}

export interface UptimeTargetDTO {
  id: string
  name: string
  kind: UptimeKind
  target: string
  interval_seconds: number
  created_at: string
  last: UptimeResultDTO | null
  /** [ts_ms, latency_ms | null] for the last hour; null marks a failed check. */
  latency: [number, number | null][]
}

// Site visitor analytics (docs/SPEC.md §4i). A bounded context of its own, unrelated to host
// telemetry: no cookies, no persistent visitor identity ever reaches the browser.
export interface SiteDTO {
  id: string
  name: string
  domain: string
  created_at: string
}

export type StatsRange = 'today' | '7d' | '30d'

export interface PathCountDTO {
  path: string
  count: number
}

export interface DomainCountDTO {
  domain: string
  count: number
}

export interface SiteStatsDTO {
  pageviews: number
  visitors: number
  top_pages: PathCountDTO[]
  top_referrers: DomainCountDTO[]
}
