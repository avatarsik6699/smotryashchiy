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
/**
 * Schedules a published snapshot: the next animation frame, so a burst of stream frames (an agent
 * batch is dozens of metrics) renders once, and a hidden tab renders not at all until it is shown
 * (Change 38). Without animation frames (tests, old engines) a short timer does the same.
 */
const defaultSchedule = (publish: () => void): void => {
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(publish)
  else setTimeout(publish, 16)
}

const isHidden = (): boolean => typeof document !== 'undefined' && document.hidden

export class DashboardStore {
  private state: DashboardState = initialState
  /** What subscribers read: the state as of the last publish. */
  private published: DashboardState = initialState
  private publishQueued = false
  private listeners = new Set<Listener>()
  private loading: Promise<void> | null = null
  private refreshTimer: ReturnType<typeof setInterval> | null = null
  private hostRefreshQueued = false

  constructor(
    private readonly now: () => number = Date.now,
    private readonly schedule: (publish: () => void) => void = defaultSchedule,
  ) {}

  subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot = (): DashboardState => this.published

  /** Replaces the state; loads publish at once, stream frames wait for the next scheduled publish. */
  private set(next: DashboardState, deferred = false) {
    if (next === this.state) return
    this.state = next
    if (!deferred) {
      this.publish()
      return
    }
    if (this.publishQueued) return
    this.publishQueued = true
    this.schedule(() => this.publish())
  }

  private publish = () => {
    this.publishQueued = false
    if (this.published === this.state) return
    this.published = this.state
    for (const l of this.listeners) l()
  }

  /**
   * Starts the periodic refresh and performs the first load. A hidden tab skips its refreshes and
   * catches up once when it is shown again (Change 38: background tabs kept polling every minute).
   */
  start(): void {
    void this.load()
    if (this.refreshTimer !== null) return
    this.refreshTimer = setInterval(() => {
      if (isHidden()) this.missedRefresh = true
      else void this.load()
    }, REFRESH_INTERVAL_MS)
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', this.onVisibility)
  }

  stop(): void {
    if (this.refreshTimer !== null) clearInterval(this.refreshTimer)
    this.refreshTimer = null
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', this.onVisibility)
  }

  private missedRefresh = false

  private onVisibility = () => {
    if (isHidden() || !this.missedRefresh) return
    this.missedRefresh = false
    void this.load()
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
      const [hosts, latest, events, errorLists, checks, uptime, history] = await Promise.all([
        api<{ hosts: HostDTO[] }>('/api/hosts'),
        api<{ metrics: MetricDTO[] }>(`/api/metrics?latest=true&limit=${METRIC_LIMIT}`),
        api<{ events: EventDTO[] }>(`/api/events?limit=${MAX_EVENTS}`),
        // Only the last hour counts (Change 38: without `from` a rare level walked all history).
        Promise.all(ERROR_LEVELS.map((level) => api<{ events: EventDTO[] }>(`/api/events?level=${level}&from=${encodeURIComponent(from)}&limit=${MAX_ERROR_EVENTS}`))),
        api<{ checks: CheckDTO[] }>('/api/checks'),
        api<{ targets: UptimeTargetDTO[] }>('/api/uptime'),
        this.loadHistory(from),
      ])
      this.set({
        ...this.state,
        status: 'ready',
        error: null,
        hosts: buildRecords({ hosts: hosts.hosts, latest: latest.metrics, history, events: events.events, checks: checks.checks }),
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

  private async loadHistory(from: string): Promise<MetricDTO[]> {
    const points: MetricDTO[] = []
    // Each history query reads the last hour; SQLite serves one connection at a time.
    // Limit queued reads from one dashboard while retaining every catalog series.
    for (let i = 0; i < HISTORY_METRICS.length; i += 2) {
      const batch = await Promise.all(HISTORY_METRICS.slice(i, i + 2).map((name) =>
        api<{ metrics: MetricDTO[] }>(`/api/metrics?name=${encodeURIComponent(name)}&from=${encodeURIComponent(from)}&step=${HISTORY_STEP_SECONDS}&limit=${METRIC_LIMIT}`),
      ))
      points.push(...batch.flatMap((result) => result.metrics))
    }
    return points
  }

  setConnection(connection: ConnectionState): void {
    if (connection === this.state.connection) return
    this.set({ ...this.state, connection })
  }

  /** Applies one stream frame; a frame from an unknown host schedules a reload to pick the host up. */
  handleFrame(frame: StreamFrame): void {
    const { state, unknownHost } = applyFrame(this.state, frame, this.now())
    this.set(state, true)
    if (unknownHost && !this.hostRefreshQueued) {
      this.hostRefreshQueued = true
      setTimeout(() => {
        this.hostRefreshQueued = false
        void this.load()
      }, 500)
    }
  }
}
