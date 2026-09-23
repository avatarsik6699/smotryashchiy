import { describe, expect, it } from 'vitest'
import type { CheckDTO, EventDTO, HostDTO, MetricDTO, UptimeResultDTO, UptimeTargetDTO } from '../domain/types'
import { applyFrame, buildRecords, buildTargets, currentValue, errorsLastHour, hostView, initialState, isRoutineSshRejection, routineSshRejectionsLastHour, MAX_EVENTS, networkPoints, seriesKey, type DashboardState } from './model'

const NOW = Date.UTC(2026, 8, 21, 12, 0, 0)
const iso = (offsetSeconds: number) => new Date(NOW + offsetSeconds * 1000).toISOString()
const host = (id: string, name: string, lastSeen: string | null = null): HostDTO => ({ id, name, created_at: iso(-9999), last_seen_at: lastSeen })
const metric = (h: string, name: string, offset: number, value: number, labels: Record<string, string> = {}): MetricDTO => ({ host: h, name, ts: iso(offset), value, labels })

function ready(hosts: ReturnType<typeof buildRecords>): DashboardState {
  return { ...initialState, status: 'ready', hosts }
}

describe('seriesKey', () => {
  it('is stable regardless of label order', () => {
    expect(seriesKey('m', { b: '2', a: '1' })).toBe(seriesKey('m', { a: '1', b: '2' }))
    expect(seriesKey('m', {})).toBe('m')
  })
})

describe('buildRecords', () => {
  it('sorts hosts by name, keeps null last_seen as never seen and ignores metrics of unknown hosts', () => {
    const recs = buildRecords({
      hosts: [host('b', 'zeta'), host('a', 'alpha', iso(-5))],
      latest: [metric('ghost', 'cpu.usage_percent', 0, 5)],
      history: [],
      events: [],
      checks: [],
    })
    expect(recs.map((r) => r.host.name)).toEqual(['alpha', 'zeta'])
    expect(recs[0]!.lastSeenMs).toBe(NOW - 5000)
    expect(recs[1]!.lastSeenMs).toBeNull()
    expect(recs.every((r) => r.series.size === 0)).toBe(true)
  })

  it('tops history up with the latest sample without duplicating it, and orders history by time', () => {
    const recs = buildRecords({
      hosts: [host('a', 'alpha')],
      history: [metric('a', 'cpu.usage_percent', -60, 10), metric('a', 'cpu.usage_percent', -120, 5)],
      latest: [metric('a', 'cpu.usage_percent', -60, 10), metric('a', 'cpu.usage_percent', -5, 30), metric('a', 'uptime.seconds', -5, 100)],
      events: [],
      checks: [],
    })
    const cpu = recs[0]!.series.get('cpu.usage_percent')!
    expect(cpu.points.map((p) => p.v)).toEqual([5, 10, 30])
    expect(recs[0]!.series.get('uptime.seconds')!.points).toHaveLength(1)
  })

  it('attaches checks to their host', () => {
    const check: CheckDTO = { host: 'a', name: 'disk.root', ts: iso(-3), status: 'ok', meta: {} }
    const recs = buildRecords({ hosts: [host('a', 'alpha')], history: [], latest: [], events: [], checks: [check] })
    expect(recs[0]!.checks).toEqual([check])
  })
})

describe('applyFrame', () => {
  const base = () => ready(buildRecords({ hosts: [host('a', 'alpha', iso(-60))], history: [metric('a', 'cpu.usage_percent', -60, 10)], latest: [], events: [], checks: [] }))

  it('appends a metric point, advances last seen and keeps the old snapshot untouched', () => {
    const before = base()
    const { state, unknownHost } = applyFrame(before, { type: 'metric', host_id: 'a', record: metric('a', 'cpu.usage_percent', 0, 42) }, NOW)
    expect(unknownHost).toBe(false)
    expect(state.hosts[0]!.series.get('cpu.usage_percent')!.points.map((p) => p.v)).toEqual([10, 42])
    expect(state.hosts[0]!.lastSeenMs).toBe(NOW)
    expect(before.hosts[0]!.series.get('cpu.usage_percent')!.points).toHaveLength(1) // immutable update
  })

  it('stores a measured zero', () => {
    const { state } = applyFrame(base(), { type: 'metric', host_id: 'a', record: metric('a', 'cpu.usage_percent', 0, 0) }, NOW)
    expect(currentValue(state.hosts[0]!, 'cpu.usage_percent', NOW)).toBe(0)
  })

  it('ignores replayed and out-of-order samples', () => {
    const start = base()
    const replay = applyFrame(start, { type: 'metric', host_id: 'a', record: metric('a', 'cpu.usage_percent', -60, 99) }, NOW)
    expect(replay.state).toBe(start)
    const older = applyFrame(start, { type: 'metric', host_id: 'a', record: metric('a', 'cpu.usage_percent', -90, 99) }, NOW)
    expect(older.state).toBe(start)
  })

  it('drops points older than the one-hour window when appending', () => {
    const start = ready(buildRecords({ hosts: [host('a', 'alpha')], history: [metric('a', 'cpu.usage_percent', -4000, 1), metric('a', 'cpu.usage_percent', -100, 2)], latest: [], events: [], checks: [] }))
    const { state } = applyFrame(start, { type: 'metric', host_id: 'a', record: metric('a', 'cpu.usage_percent', 0, 3) }, NOW)
    expect(state.hosts[0]!.series.get('cpu.usage_percent')!.points.map((p) => p.v)).toEqual([2, 3])
  })

  it('reports frames of unknown hosts so the caller can reload the host list', () => {
    const start = base()
    const { state, unknownHost } = applyFrame(start, { type: 'metric', host_id: 'new-host', record: metric('new-host', 'cpu.usage_percent', 0, 1) }, NOW)
    expect(unknownHost).toBe(true)
    expect(state).toBe(start)
  })

  it('replaces a check by name and marks the host seen', () => {
    const c = (status: CheckDTO['status'], offset: number): CheckDTO => ({ host: 'a', name: 'disk.root', ts: iso(offset), status, meta: {} })
    const first = applyFrame(base(), { type: 'check', host_id: 'a', record: c('ok', -5) }, NOW).state
    const second = applyFrame(first, { type: 'check', host_id: 'a', record: c('critical', 0) }, NOW).state
    expect(second.hosts[0]!.checks).toHaveLength(1)
    expect(second.hosts[0]!.checks[0]!.status).toBe('critical')
    expect(second.hosts[0]!.lastSeenMs).toBe(NOW)
  })

  it('prepends events newest first and caps the list', () => {
    let state = base()
    for (let i = 0; i < MAX_EVENTS + 5; i++) {
      const e: EventDTO = { host: 'a', ts: iso(i), level: 'info', message: `m${i}`, labels: {} }
      state = applyFrame(state, { type: 'event', host_id: 'a', record: e }, NOW).state
    }
    expect(state.events).toHaveLength(MAX_EVENTS)
    expect(state.events[0]!.message).toBe(`m${MAX_EVENTS + 4}`)
  })
})

describe('selectors', () => {
  it('reports unknown (null) for a stale sample instead of showing it as current', () => {
    const rec = buildRecords({ hosts: [host('a', 'alpha')], history: [], latest: [metric('a', 'cpu.usage_percent', -400, 50)], events: [], checks: [] })[0]!
    expect(currentValue(rec, 'cpu.usage_percent', NOW)).toBeNull()
    expect(currentValue(rec, 'cpu.usage_percent', NOW - 300_000)).toBe(50)
    expect(currentValue(rec, 'memory.used_percent', NOW)).toBeNull()
  })

  it('derives network throughput from counters summed over interfaces', () => {
    const c = (name: string, iface: string, offset: number, value: number) => metric('a', name, offset, value, { interface: iface })
    const rec = buildRecords({
      hosts: [host('a', 'alpha')],
      history: [
        c('network.rx_bytes_total', 'eth0', -20, 1000), c('network.rx_bytes_total', 'eth0', -10, 3000),
        c('network.tx_bytes_total', 'eth0', -20, 0), c('network.tx_bytes_total', 'eth0', -10, 500),
        c('network.rx_bytes_total', 'eth1', -20, 0), c('network.rx_bytes_total', 'eth1', -10, 100),
      ],
      latest: [],
      events: [],
      checks: [],
    })[0]!
    expect(networkPoints(rec)).toEqual([{ t: NOW - 10_000, v: 200 + 50 + 10 }])
    expect(hostView(rec, NOW).network.rate).toBe(260)
  })

  it('builds a host view with the fullest disk mount and unknowns as null', () => {
    const d = (mount: string, v: number) => metric('a', 'disk.used_percent', -5, v, { mount, device: `/dev/${mount}` })
    const rec = buildRecords({ hosts: [host('a', 'alpha')], history: [], latest: [d('/', 41), d('/data', 88), metric('a', 'uptime.seconds', -5, 3600)], events: [], checks: [] })[0]!
    const view = hostView(rec, NOW)
    expect(view.disk).toMatchObject({ mount: '/data', value: 88 })
    expect(view.uptimeSeconds).toBe(3600)
    expect(view.cpu.value).toBeNull()
    expect(view.network.rate).toBeNull()
  })
})

describe('uptime targets', () => {
  const result = (offset: number, ok: boolean, latency: number | null): UptimeResultDTO => ({ target_id: 't1', ts: iso(offset), ok, latency_ms: latency, status_code: ok ? 200 : null, error: ok ? '' : 'connection refused', cert_expires_at: null })
  const dto = (over: Partial<UptimeTargetDTO> = {}): UptimeTargetDTO => ({
    id: 't1', name: 'site', kind: 'http', target: 'https://example.com', interval_seconds: 60, created_at: iso(-9999),
    last: result(-10, true, 120), latency: [[NOW - 20_000, 100], [NOW - 10_000, null]], ...over,
  })
  const withTargets = (list: UptimeTargetDTO[]): DashboardState => ({ ...initialState, status: 'ready', targets: buildTargets(list) })

  it('maps the API shape, keeping null latency as null (not 0)', () => {
    const [rec] = buildTargets([dto()])
    expect(rec!.latency).toEqual([{ t: NOW - 20_000, v: 100 }, { t: NOW - 10_000, v: null }])
    expect(rec!.target.name).toBe('site')
  })

  it('applies a live result: newest becomes last and a failed check adds a null point', () => {
    const start = withTargets([dto()])
    const { state } = applyFrame(start, { type: 'uptime', target_id: 't1', record: result(0, false, null) }, NOW)
    expect(state.targets[0]!.last).toMatchObject({ ok: false, error: 'connection refused' })
    expect(state.targets[0]!.latency.at(-1)).toEqual({ t: NOW, v: null })
    expect(start.targets[0]!.latency).toHaveLength(2) // the old snapshot is untouched
  })

  it('ignores a replayed or older result', () => {
    const start = withTargets([dto()])
    expect(applyFrame(start, { type: 'uptime', target_id: 't1', record: result(-10, false, null) }, NOW).state).toBe(start)
    expect(applyFrame(start, { type: 'uptime', target_id: 't1', record: result(-50, false, null) }, NOW).state).toBe(start)
  })

  it('reports a result of an unknown target so the caller reloads', () => {
    const start = withTargets([dto()])
    const out = applyFrame(start, { type: 'uptime', target_id: 'other', record: { ...result(0, true, 5), target_id: 'other' } }, NOW)
    expect(out.unknownHost).toBe(true)
    expect(out.state).toBe(start)
  })

  it('drops history older than the one-hour window', () => {
    const start = withTargets([dto({ latency: [[NOW - 4000_000, 1], [NOW - 10_000, 2]] })])
    const { state } = applyFrame(start, { type: 'uptime', target_id: 't1', record: result(0, true, 3) }, NOW)
    expect(state.targets[0]!.latency.map((p) => p.v)).toEqual([2, 3])
  })
})

describe('errorsLastHour', () => {
  it('counts error and critical events from the load and live frames within the hour', () => {
    const ev = (minutesAgo: number, level: EventDTO['level']): EventDTO => ({ host: 'a', ts: new Date(NOW - minutesAgo * 60_000).toISOString(), level, message: level, labels: {} })
    let state: DashboardState = { ...initialState, status: 'ready', errors: [ev(30, 'error'), ev(90, 'error')] }
    for (const e of [ev(1, 'critical'), ev(1, 'warn'), ev(1, 'info')]) state = applyFrame(state, { type: 'event', host_id: 'a', record: e }, NOW).state
    expect(errorsLastHour(state, NOW)).toBe(2)
    expect(errorsLastHour({ ...initialState }, NOW)).toBeNull()
  })
})

describe('routine SSH pre-auth rejections (Change 23)', () => {
  const ev = (unit: string | null, message: string, level: EventDTO['level'] = 'error'): EventDTO => ({
    host: 'a',
    ts: new Date(NOW - 60_000).toISOString(),
    level,
    message,
    labels: unit === null ? {} : { unit },
  })
  const bot = 'error: maximum authentication attempts exceeded for root from 45.148.10.152 port 43992 ssh2 [preauth]'

  it('recognizes sshd pre-auth lines from any sshd unit name, and nothing else', () => {
    for (const unit of ['ssh', 'ssh.service', 'sshd', 'sshd.service']) expect(isRoutineSshRejection(ev(unit, bot))).toBe(true)
    expect(isRoutineSshRejection(ev('ssh.service', bot, 'critical'))).toBe(true)
    expect(isRoutineSshRejection(ev('ssh.service', 'error: kex_exchange_identification: read: Connection reset'))).toBe(false) // not pre-auth-tagged
    expect(isRoutineSshRejection(ev('nginx.service', bot))).toBe(false)
    expect(isRoutineSshRejection(ev(null, bot))).toBe(false)
    expect(isRoutineSshRejection(ev('ssh.service', bot, 'warn'))).toBe(false) // not an error at all
  })

  it('counts them apart from real errors', () => {
    const state: DashboardState = {
      ...initialState,
      status: 'ready',
      errors: [ev('ssh.service', bot), ev('sshd', bot), ev('apt-daily.service', 'Timeout occurred while waiting for network connectivity.')],
    }
    expect(errorsLastHour(state, NOW)).toBe(1)
    expect(routineSshRejectionsLastHour(state, NOW)).toBe(2)
    expect(routineSshRejectionsLastHour({ ...initialState }, NOW)).toBeNull()
  })
})
