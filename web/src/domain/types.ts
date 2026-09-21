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
