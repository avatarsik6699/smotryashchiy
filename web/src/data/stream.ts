import { api, signalUnauthorized } from '../api/client'
import type { DashboardStore } from './store'
import type { StreamFrame } from './model'

/** Consecutive failed connection attempts after which the UI says "offline" instead of "reconnecting". */
export const OFFLINE_AFTER_FAILURES = 3
const BACKOFF_MIN_MS = 1000
const BACKOFF_MAX_MS = 10_000

export function backoffDelay(failures: number, jitter: number): number {
  const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** Math.max(0, failures - 1))
  return Math.round(base * (0.8 + 0.4 * jitter))
}

export function streamUrl(loc: Pick<Location, 'protocol' | 'host'> = window.location): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/api/stream`
}

/** Type guard for a server frame (docs/SPEC.md §4.6); anything else is ignored, never applied. */
export function parseFrame(data: unknown): StreamFrame | null {
  if (typeof data !== 'string') return null
  let value: unknown
  try {
    value = JSON.parse(data)
  } catch {
    return null
  }
  if (!value || typeof value !== 'object') return null
  const f = value as Record<string, unknown>
  if ((f.type !== 'metric' && f.type !== 'check' && f.type !== 'event') || typeof f.host_id !== 'string') return null
  if (!f.record || typeof f.record !== 'object') return null
  return f as unknown as StreamFrame
}

interface Options {
  url?: string
  createSocket?: (url: string) => WebSocket
  random?: () => number
  /** Asks whether a session still exists; defaults to GET /api/auth/session. */
  probeSession?: () => Promise<boolean>
  onSessionLost?: () => void
}

/**
 * Keeps the live stream connected: frames go to the store, and every (re)connection triggers a full
 * refresh so nothing missed while disconnected stays missing. It reports connecting / live /
 * reconnecting / offline as text-able state.
 */
export class StreamClient {
  private socket: WebSocket | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private failures = 0
  private stopped = true
  private everLive = false

  constructor(
    private readonly store: DashboardStore,
    private readonly options: Options = {},
  ) {}

  start(): void {
    if (!this.stopped) return
    this.stopped = false
    this.connect()
  }

  stop(): void {
    this.stopped = true
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
    const s = this.socket
    this.socket = null
    if (s) {
      s.onopen = s.onmessage = s.onclose = s.onerror = null
      s.close()
    }
  }

  private connect(): void {
    const url = this.options.url ?? streamUrl()
    const socket = (this.options.createSocket ?? ((u) => new WebSocket(u)))(url)
    this.socket = socket
    socket.onopen = () => {
      this.failures = 0
      this.everLive = true
      this.store.setConnection('live')
      void this.store.load() // close any gap left by a previous disconnect
    }
    socket.onmessage = (event: MessageEvent) => {
      const frame = parseFrame(event.data)
      if (frame) this.store.handleFrame(frame)
    }
    socket.onclose = () => this.dropped(socket)
    socket.onerror = () => {
      // The close event follows and drives the retry.
    }
  }

  /**
   * A refused WebSocket upgrade looks like any other failure to the browser. After repeated failures
   * ask the cheap session endpoint whether the session is gone (e.g. the server restarted); if so the
   * app shows the login form instead of waiting for the next full refresh.
   */
  private checkSession(): void {
    const probe = this.options.probeSession ?? (async () => (await api<{ authenticated: boolean }>('/api/auth/session')).authenticated)
    const lost = this.options.onSessionLost ?? signalUnauthorized
    probe().then(
      (authenticated) => {
        if (!authenticated && !this.stopped) lost()
      },
      () => undefined, // server unreachable: not the same as a lost session
    )
  }

  private dropped(socket: WebSocket): void {
    if (this.stopped || socket !== this.socket) return
    this.socket = null
    this.failures += 1
    this.store.setConnection(this.failures >= OFFLINE_AFTER_FAILURES ? 'offline' : this.everLive || this.failures > 1 ? 'reconnecting' : 'connecting')
    if (this.failures >= 2) this.checkSession()
    const delay = backoffDelay(this.failures, (this.options.random ?? Math.random)())
    this.timer = setTimeout(() => {
      this.timer = null
      if (!this.stopped) this.connect()
    }, delay)
  }
}
