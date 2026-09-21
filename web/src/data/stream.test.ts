import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { backoffDelay, OFFLINE_AFTER_FAILURES, parseFrame, StreamClient, streamUrl } from './stream'
import type { DashboardStore } from './store'

class FakeSocket {
  static instances: FakeSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(readonly url: string) {
    FakeSocket.instances.push(this)
  }
  close() {
    this.closed = true
  }
  open() {
    this.onopen?.()
  }
  message(data: unknown) {
    this.onmessage?.({ data } as MessageEvent)
  }
  drop() {
    this.onclose?.()
  }
}

function fakeStore() {
  return { setConnection: vi.fn(), handleFrame: vi.fn(), load: vi.fn().mockResolvedValue(undefined) }
}

function client(store = fakeStore()) {
  const sc = new StreamClient(store as unknown as DashboardStore, { url: 'ws://x/api/stream', createSocket: (u) => new FakeSocket(u) as unknown as WebSocket, random: () => 0.5, probeSession: () => Promise.resolve(true) })
  return { sc, store }
}

beforeEach(() => {
  FakeSocket.instances = []
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
})

describe('parseFrame', () => {
  it('accepts server frames and rejects everything else', () => {
    const good = { type: 'metric', host_id: 'h', record: { name: 'a.b' } }
    expect(parseFrame(JSON.stringify(good))).toEqual(good)
    for (const bad of ['not json', '{}', '[]', 'null', JSON.stringify({ type: 'nope', host_id: 'h', record: {} }), JSON.stringify({ type: 'metric', record: {} }), JSON.stringify({ type: 'metric', host_id: 'h' })]) {
      expect(parseFrame(bad)).toBeNull()
    }
    expect(parseFrame(new ArrayBuffer(1))).toBeNull()
    const uptime = { type: 'uptime', target_id: 't1', record: { ok: true } }
    expect(parseFrame(JSON.stringify(uptime))).toEqual(uptime)
    expect(parseFrame(JSON.stringify({ type: 'uptime', record: {} }))).toBeNull() // needs a target_id, not a host_id
    expect(parseFrame(JSON.stringify({ type: 'uptime', host_id: 'h', record: {} }))).toBeNull()
  })
})

describe('streamUrl / backoff', () => {
  it('picks ws or wss from the page protocol', () => {
    expect(streamUrl({ protocol: 'http:', host: 'a:1' })).toBe('ws://a:1/api/stream')
    expect(streamUrl({ protocol: 'https:', host: 'a' })).toBe('wss://a/api/stream')
  })
  it('doubles up to a 10 s cap and jitters ±20 %', () => {
    expect(backoffDelay(1, 0.5)).toBe(1000)
    expect(backoffDelay(3, 0.5)).toBe(4000)
    expect(backoffDelay(50, 0.5)).toBe(10_000)
    expect(backoffDelay(1, 0)).toBe(800)
    expect(backoffDelay(1, 1)).toBe(1200)
  })
})

describe('StreamClient session probe', () => {
  function withProbe(probe: () => Promise<boolean>) {
    const store = fakeStore()
    const onSessionLost = vi.fn()
    const sc = new StreamClient(store as unknown as DashboardStore, { url: 'ws://x/api/stream', createSocket: (u) => new FakeSocket(u) as unknown as WebSocket, random: () => 0.5, probeSession: probe, onSessionLost })
    return { sc, onSessionLost }
  }

  it('does not probe on the first failure, but asks about the session from the second on', async () => {
    const probe = vi.fn().mockResolvedValue(true)
    const { sc } = withProbe(probe)
    sc.start()
    FakeSocket.instances[0]!.drop()
    expect(probe).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1500)
    FakeSocket.instances[1]!.drop()
    expect(probe).toHaveBeenCalledTimes(1)
  })

  it('reports a lost session so the app can show the login form', async () => {
    const { sc, onSessionLost } = withProbe(() => Promise.resolve(false))
    sc.start()
    FakeSocket.instances[0]!.drop()
    vi.advanceTimersByTime(1500)
    FakeSocket.instances[1]!.drop()
    await vi.advanceTimersByTimeAsync(0)
    expect(onSessionLost).toHaveBeenCalledTimes(1)
  })

  it('does not report a lost session when the session exists or the server is unreachable', async () => {
    for (const probe of [() => Promise.resolve(true), () => Promise.reject(new Error('down'))]) {
      const { sc, onSessionLost } = withProbe(probe)
      sc.start()
      FakeSocket.instances.at(-1)!.drop()
      vi.advanceTimersByTime(1500)
      FakeSocket.instances.at(-1)!.drop()
      await vi.advanceTimersByTimeAsync(0)
      expect(onSessionLost).not.toHaveBeenCalled()
      sc.stop()
    }
  })
})

describe('StreamClient', () => {
  it('goes live on open, refreshes everything and forwards valid frames only', () => {
    const { sc, store } = client()
    sc.start()
    const socket = FakeSocket.instances[0]!
    expect(socket.url).toBe('ws://x/api/stream')
    socket.open()
    expect(store.setConnection).toHaveBeenLastCalledWith('live')
    expect(store.load).toHaveBeenCalledTimes(1)
    socket.message(JSON.stringify({ type: 'metric', host_id: 'h', record: { name: 'a.b' } }))
    socket.message('garbage')
    expect(store.handleFrame).toHaveBeenCalledTimes(1)
  })

  it('shows reconnecting after a live connection drops, reconnects with backoff and refreshes again', () => {
    const { sc, store } = client()
    sc.start()
    FakeSocket.instances[0]!.open()
    FakeSocket.instances[0]!.drop()
    expect(store.setConnection).toHaveBeenLastCalledWith('reconnecting')
    vi.advanceTimersByTime(999)
    expect(FakeSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(2)
    expect(FakeSocket.instances).toHaveLength(2)
    FakeSocket.instances[1]!.open()
    expect(store.setConnection).toHaveBeenLastCalledWith('live')
    expect(store.load).toHaveBeenCalledTimes(2)
  })

  it('says offline after repeated failures but keeps retrying', () => {
    const { sc, store } = client()
    sc.start()
    for (let i = 0; i < OFFLINE_AFTER_FAILURES; i++) {
      FakeSocket.instances.at(-1)!.drop()
      vi.advanceTimersByTime(40_000)
    }
    expect(store.setConnection).toHaveBeenCalledWith('offline')
    expect(FakeSocket.instances.length).toBeGreaterThan(OFFLINE_AFTER_FAILURES)
    FakeSocket.instances.at(-1)!.open()
    expect(store.setConnection).toHaveBeenLastCalledWith('live')
  })

  it('stops for good: no reconnect after stop, and the socket is closed', () => {
    const { sc } = client()
    sc.start()
    const socket = FakeSocket.instances[0]!
    sc.stop()
    expect(socket.closed).toBe(true)
    socket.drop()
    vi.advanceTimersByTime(60_000)
    expect(FakeSocket.instances).toHaveLength(1)
  })

  it('ignores a stale socket closing after a newer one took over', () => {
    const { sc, store } = client()
    sc.start()
    const first = FakeSocket.instances[0]!
    first.open()
    first.drop()
    vi.advanceTimersByTime(1500)
    const second = FakeSocket.instances[1]!
    second.open()
    store.setConnection.mockClear()
    first.drop() // late close event of the dead socket
    expect(store.setConnection).not.toHaveBeenCalled()
  })
})
