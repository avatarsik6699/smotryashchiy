import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DashboardStore } from './store'

const NOW = Date.UTC(2026, 8, 21, 12, 0, 0)
const fetchMock = vi.fn<typeof fetch>()

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 })
}

function routeAll(overrides: Record<string, () => Response> = {}) {
  fetchMock.mockImplementation(async (input) => {
    const url = String(input)
    for (const [prefix, make] of Object.entries(overrides)) if (url.startsWith(prefix)) return make()
    if (url.startsWith('/api/hosts')) return ok({ hosts: [{ id: 'a', name: 'alpha', created_at: new Date(NOW).toISOString(), last_seen_at: null }] })
    if (url.startsWith('/api/metrics')) return ok({ metrics: [] })
    if (url.startsWith('/api/events')) return ok({ events: [] })
    if (url.startsWith('/api/checks')) return ok({ checks: [] })
    if (url.startsWith('/api/uptime')) return ok({ targets: [] })
    throw new Error(`unexpected ${url}`)
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('DashboardStore.load', () => {
  it('loads error and critical events for the last-hour error count (Change 22)', async () => {
    const ev = (ts: string, level: string) => ({ host: 'a', ts, level, message: level, labels: {} })
    routeAll({
      '/api/events?level=error': () => ok({ events: [ev('2026-09-21T11:50:00Z', 'error'), ev('2026-09-21T10:00:00Z', 'error')] }),
      '/api/events?level=critical': () => ok({ events: [ev('2026-09-21T11:55:00Z', 'critical')] }),
    })
    const store = new DashboardStore(() => NOW)
    await store.load()
    expect(store.getSnapshot().errors.map((e) => e.level + ' ' + e.ts)).toEqual([
      'critical 2026-09-21T11:55:00Z',
      'error 2026-09-21T11:50:00Z',
      'error 2026-09-21T10:00:00Z',
    ])
  })

  it('requests hosts, latest, one stepped history per catalog metric, events and checks', async () => {
    routeAll()
    const store = new DashboardStore(() => NOW)
    await store.load()
    const urls = fetchMock.mock.calls.map((c) => String(c[0]))
    expect(urls).toContain('/api/hosts')
    expect(urls.some((u) => u.startsWith('/api/metrics?latest=true'))).toBe(true)
    expect(urls.some((u) => u.startsWith('/api/events?limit=50'))).toBe(true)
    expect(urls).toContain('/api/checks')
    expect(urls).toContain('/api/uptime')
    const history = urls.filter((u) => u.includes('step=60'))
    expect(history).toHaveLength(5)
    for (const name of ['cpu.usage_percent', 'memory.used_percent', 'disk.used_percent', 'network.rx_bytes_total', 'network.tx_bytes_total']) {
      expect(history.some((u) => u.includes(`name=${name}`))).toBe(true)
    }
    expect(history[0]).toContain(`from=${encodeURIComponent(new Date(NOW - 3600_000).toISOString())}`)
    expect(store.getSnapshot().status).toBe('ready')
    expect(store.getSnapshot().hosts.map((h) => h.host.name)).toEqual(['alpha'])
  })

  it('shares one request set between concurrent loads', async () => {
    routeAll()
    const store = new DashboardStore(() => NOW)
    await Promise.all([store.load(), store.load(), store.load()])
    expect(fetchMock.mock.calls.filter((c) => String(c[0]) === '/api/hosts')).toHaveLength(1)
  })

  it('is an error state when the first load fails, and keeps data with an error when a refresh fails', async () => {
    routeAll({ '/api/hosts': () => new Response('{"error":"boom"}', { status: 500 }) })
    const store = new DashboardStore(() => NOW)
    await store.load()
    expect(store.getSnapshot()).toMatchObject({ status: 'error', error: 'boom' })

    routeAll()
    await store.load()
    expect(store.getSnapshot()).toMatchObject({ status: 'ready', error: null })
    routeAll({ '/api/events': () => new Response('{"error":"db down"}', { status: 500 }) })
    await store.load()
    const snap = store.getSnapshot()
    expect(snap.status).toBe('ready')
    expect(snap.error).toBe('db down')
    expect(snap.hosts).toHaveLength(1) // what was on screen stays on screen
  })

  it('notifies subscribers and stops after unsubscribing', async () => {
    routeAll()
    const store = new DashboardStore(() => NOW)
    const listener = vi.fn()
    const off = store.subscribe(listener)
    await store.load()
    expect(listener).toHaveBeenCalled()
    listener.mockClear()
    off()
    store.setConnection('live')
    expect(listener).not.toHaveBeenCalled()
  })

  it('refreshes on an interval and stops when stopped', async () => {
    vi.useFakeTimers()
    routeAll()
    const store = new DashboardStore(() => NOW)
    store.start()
    await vi.advanceTimersByTimeAsync(0)
    const first = fetchMock.mock.calls.filter((c) => String(c[0]) === '/api/hosts').length
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchMock.mock.calls.filter((c) => String(c[0]) === '/api/hosts').length).toBe(first + 1)
    store.stop()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(fetchMock.mock.calls.filter((c) => String(c[0]) === '/api/hosts').length).toBe(first + 1)
  })
})

describe('DashboardStore.handleFrame', () => {
  it('reloads once shortly after a frame from an unknown host', async () => {
    vi.useFakeTimers()
    routeAll()
    const store = new DashboardStore(() => NOW)
    await store.load()
    fetchMock.mockClear()
    const frame = { type: 'metric' as const, host_id: 'newcomer', record: { host: 'newcomer', name: 'cpu.usage_percent', ts: new Date(NOW).toISOString(), value: 1, labels: {} } }
    store.handleFrame(frame)
    store.handleFrame(frame)
    await vi.advanceTimersByTimeAsync(600)
    expect(fetchMock.mock.calls.filter((c) => String(c[0]) === '/api/hosts')).toHaveLength(1)
  })
})
