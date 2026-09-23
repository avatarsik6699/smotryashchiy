import { api } from '../api/client'
import type { CheckDTO, EventDTO, HostDTO, MetricDTO, UptimeTargetDTO } from '../domain/types'
import {
  applyFrame,
  buildRecords,
  buildTargets,
  HISTORY_METRICS,
  initialState,
  ERROR_LEVELS,
  isErrorEvent,
  MAX_ERROR_EVENTS,
  MAX_EVENTS,
  WINDOW_MS,
  type ConnectionState,
  type DashboardState,
  type StreamFrame,
} from './model'

/** Refresh cadence of the full load (docs/SPEC.md §5). */
export const REFRESH_INTERVAL_MS = 60_000
const HISTORY_STEP_SECONDS = 60
const METRIC_LIMIT = 5000

type Listener = () => void

/**
 * The dashboard's data: one immutable snapshot, replaced on every change, read through
 * subscribe/getSnapshot (useSyncExternalStore). Loading is a full refresh; the stream applies
 * incremental frames in between.
 */
export class DashboardStore {
  private state: DashboardState = initialState
  private listeners = new Set<Listener>()
  private loading: Promise<void> | null = null
  private refreshTimer: ReturnType<typeof setInterval> | null = null
  private hostRefreshQueued = false

  constructor(private readonly now: () => number = Date.now) {}

  subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot = (): DashboardState => this.state

  private set(next: DashboardState) {
    if (next === this.state) return
    this.state = next
    for (const l of this.listeners) l()
  }

  /** Starts the periodic refresh and performs the first load. */
  start(): void {
    void this.load()
    if (this.refreshTimer === null) this.refreshTimer = setInterval(() => void this.load(), REFRESH_INTERVAL_MS)
  }

  stop(): void {
    if (this.refreshTimer !== null) clearInterval(this.refreshTimer)
    this.refreshTimer = null
  }

  /** Full load; concurrent calls share one request set. */
  load(): Promise<void> {
    if (this.loading) return this.loading
    this.loading = this.doLoad().finally(() => {
      this.loading = null
    })
    return this.loading
  }

  private async doLoad(): Promise<void> {
    const from = new Date(this.now() - WINDOW_MS).toISOString()
    try {
      const [hosts, latest, events, errorLists, checks, uptime, ...history] = await Promise.all([
        api<{ hosts: HostDTO[] }>('/api/hosts'),
        api<{ metrics: MetricDTO[] }>(`/api/metrics?latest=true&limit=${METRIC_LIMIT}`),
        api<{ events: EventDTO[] }>(`/api/events?limit=${MAX_EVENTS}`),
        Promise.all(ERROR_LEVELS.map((level) => api<{ events: EventDTO[] }>(`/api/events?level=${level}&limit=${MAX_ERROR_EVENTS}`))),
        api<{ checks: CheckDTO[] }>('/api/checks'),
        api<{ targets: UptimeTargetDTO[] }>('/api/uptime'),
        ...HISTORY_METRICS.map((name) =>
          api<{ metrics: MetricDTO[] }>(`/api/metrics?name=${encodeURIComponent(name)}&from=${encodeURIComponent(from)}&step=${HISTORY_STEP_SECONDS}&limit=${METRIC_LIMIT}`),
        ),
      ])
      this.set({
        ...this.state,
        status: 'ready',
        error: null,
        hosts: buildRecords({ hosts: hosts.hosts, latest: latest.metrics, history: history.flatMap((h) => h.metrics), events: events.events, checks: checks.checks }),
        targets: buildTargets(uptime.targets),
        events: events.events,
        errors: errorLists
          .flatMap((l) => l.events)
          .filter(isErrorEvent)
          .sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts))
          .slice(0, MAX_ERROR_EVENTS),
      })
    } catch (e) {
      const message = e instanceof Error ? e.message : 'load failed'
      this.set({ ...this.state, status: this.state.status === 'ready' ? 'ready' : 'error', error: message })
    }
  }

  setConnection(connection: ConnectionState): void {
    if (connection === this.state.connection) return
    this.set({ ...this.state, connection })
  }

  /** Applies one stream frame; a frame from an unknown host schedules a reload to pick the host up. */
  handleFrame(frame: StreamFrame): void {
    const { state, unknownHost } = applyFrame(this.state, frame, this.now())
    this.set(state)
    if (unknownHost && !this.hostRefreshQueued) {
      this.hostRefreshQueued = true
      setTimeout(() => {
        this.hostRefreshQueued = false
        void this.load()
      }, 500)
    }
  }
}
