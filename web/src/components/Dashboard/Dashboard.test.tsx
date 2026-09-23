import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { DashboardStore } from '../../data/store'
import type { EventDTO, HostDTO, MetricDTO, UptimeTargetDTO } from '../../domain/types'
import { Dashboard } from './Dashboard'

const NOW = Date.now()
const iso = (offsetMs: number) => new Date(NOW + offsetMs).toISOString()
const fetchMock = vi.fn<typeof fetch>()

const host = (id: string, name: string, lastSeenOffsetMs: number | null): HostDTO => ({
  id,
  name,
  created_at: iso(-86_400_000),
  last_seen_at: lastSeenOffsetMs === null ? null : iso(lastSeenOffsetMs),
})
const metric = (h: string, name: string, offsetMs: number, value: number, labels: Record<string, string> = {}): MetricDTO => ({ host: h, name, ts: iso(offsetMs), value, labels })
const event = (h: string, offsetMs: number, level: EventDTO['level'], message: string): EventDTO => ({ host: h, ts: iso(offsetMs), level, message, labels: {} })

interface Fixture {
  hosts: HostDTO[]
  latest: MetricDTO[]
  events: EventDTO[]
  checks?: {
    host: string
    name: string
    ts: string
    status: 'ok' | 'warn' | 'critical'
    meta: unknown
  }[]
  targets?: UptimeTargetDTO[]
}

const FULL: Fixture = {
  hosts: [host('a', 'vps-a', -5_000), host('b', 'vps-b', -3 * 60_000), host('c', 'vps-c', -3600_000), host('d', 'vps-d', null)],
  latest: [
    metric('a', 'cpu.usage_percent', -5_000, 37.4),
    metric('a', 'memory.used_percent', -5_000, 57),
    metric('a', 'disk.used_percent', -5_000, 41, {
      mount: '/',
      device: '/dev/sda1',
    }),
    metric('a', 'disk.used_percent', -5_000, 88, {
      mount: '/data',
      device: '/dev/sdb1',
    }),
    metric('a', 'disk.used_bytes', -5_000, 8 * 1024 ** 3, {
      mount: '/data',
      device: '/dev/sdb1',
    }),
    metric('a', 'disk.total_bytes', -5_000, 10 * 1024 ** 3, {
      mount: '/data',
      device: '/dev/sdb1',
    }),
    metric('a', 'uptime.seconds', -5_000, 14 * 86400 + 6 * 3600),
    metric('b', 'cpu.usage_percent', -3 * 60_000, 0), // a measured zero, still fresh enough
    metric('b', 'memory.used_percent', -3 * 60_000, 20),
    metric('c', 'cpu.usage_percent', -3600_000, 50), // an hour old: unknown now
  ],
  events: [event('a', -60_000, 'warn', 'ban 203.0.113.7'), event('c', -3600_000, 'error', 'agent stopped')],
  checks: [{ host: 'a', name: 'disk.root', ts: iso(-10_000), status: 'ok', meta: {} }],
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status })
}

function serve(fx: Fixture, overrides: Record<string, () => Response> = {}) {
  fetchMock.mockImplementation(async (input) => {
    const url = String(input)
    for (const [prefix, make] of Object.entries(overrides)) if (url.startsWith(prefix)) return make()
    if (url.startsWith('/api/hosts')) return json({ hosts: fx.hosts })
    if (url.startsWith('/api/metrics'))
      return json({
        metrics: url.includes('latest=true') ? fx.latest : url.includes('host=') ? fx.latest.filter((m) => url.includes(`host=${m.host}`) && url.includes(`name=${encodeURIComponent(m.name)}`)) : [],
      })
    if (url.startsWith('/api/events'))
      return json({
        events: url.includes('host=') ? fx.events.filter((e) => url.includes(`host=${e.host}`)) : fx.events,
      })
    if (url.startsWith('/api/checks')) return json({ checks: fx.checks ?? [] })
    if (url.startsWith('/api/uptime')) return json({ targets: fx.targets ?? [] })
    throw new Error(`unexpected ${url}`)
  })
}

async function mount(fx: Fixture = FULL, overrides: Record<string, () => Response> = {}) {
  serve(fx, overrides)
  const store = new DashboardStore()
  await store.load()
  const onLogout = vi.fn().mockResolvedValue(undefined)
  const view = render(<Dashboard store={store} onLogout={onLogout} />)
  return { store, onLogout, ...view }
}

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

const row = (name: string) => screen.getByRole('button', { name: new RegExp(name) })

describe('Dashboard states', () => {
  it('shows a loading skeleton before the first load completes', () => {
    const store = new DashboardStore()
    render(<Dashboard store={store} onLogout={vi.fn()} />)
    expect(screen.getByText('Loading hosts and events…')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Hosts' })).not.toBeInTheDocument()
  })

  it('shows the error with a retry that reloads', async () => {
    fetchMock.mockImplementation(async () => json({ error: 'db unavailable' }, 500))
    const store = new DashboardStore()
    await store.load()
    render(<Dashboard store={store} onLogout={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load the dashboard. db unavailable')
    serve(FULL)
    await userEvent.setup().click(screen.getByRole('button', { name: 'retry' }))
    expect(await screen.findByRole('heading', { name: 'Hosts' })).toBeInTheDocument()
  })

  it('keeps the data and warns when a refresh fails', async () => {
    const { store } = await mount()
    serve(FULL, { '/api/events': () => json({ error: 'timeout' }, 500) })
    await store.load()
    expect(await screen.findByText('Data may be out of date.')).toBeInTheDocument()
    expect(row('vps-a')).toBeInTheDocument()
  })

  it('invites adding a host when there are none', async () => {
    await mount({ hosts: [], latest: [], events: [] })
    expect(screen.getByText(/No hosts yet\./)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add a host' })).toBeInTheDocument()
    expect(screen.getByText('No events reported.')).toBeInTheDocument()
  })

  it('says so when live updates are offline', async () => {
    const { store } = await mount()
    store.setConnection('offline')
    expect(await screen.findByText('Live updates are offline.')).toBeInTheDocument()
  })

  it('shows the connection as text', async () => {
    const { store } = await mount()
    store.setConnection('live')
    expect(await screen.findByText('live')).toBeInTheDocument()
    store.setConnection('reconnecting')
    expect(await screen.findByText('reconnecting')).toBeInTheDocument()
  })
})

describe('STATUS strip', () => {
  it('counts hosts by freshness state and averages only known values', async () => {
    await mount()
    const strip = screen.getByRole('region', { name: 'Status' })
    const value = (label: string) => within(strip).getByText(label, { selector: 'dt' }).nextElementSibling!.firstChild!.textContent
    expect(value('hosts')).toBe('4')
    expect(value('ok')).toBe('1')
    expect(value('stale')).toBe('1')
    expect(value('offline')).toBe('1')
    // A bad state that is present is itself a finding, written out (Change 22).
    expect(within(strip).getByText('stale', { selector: 'dt' }).nextElementSibling).toHaveTextContent('watch')
    expect(within(strip).getByText('offline', { selector: 'dt' }).nextElementSibling).toHaveTextContent('problem')
    expect(value('new')).toBe('1')
    // vps-a 37.4 and vps-b 0 are known; vps-c's hour-old value and vps-d's nothing are excluded.
    expect(value('avg cpu')).toBe('19%')
    expect(value('avg mem')).toBe('39%')
  })

  it('shows — for averages when nothing is known instead of 0', async () => {
    await mount({ hosts: [host('d', 'vps-d', null)], latest: [], events: [] })
    const strip = screen.getByRole('region', { name: 'Status' })
    expect(within(strip).getByText('avg cpu', { selector: 'dt' }).nextElementSibling).toHaveTextContent('—')
  })
})

describe('HOSTS ledger', () => {
  it('names the state of each host in text and orders by name', async () => {
    await mount()
    const names = screen.getAllByRole('button').map((b) => b.textContent ?? '')
    const ledger = names.filter((t) => /vps-/.test(t))
    expect(ledger.map((t) => /vps-\w/.exec(t)![0])).toEqual(['vps-a', 'vps-b', 'vps-c', 'vps-d'])
    expect(row('vps-a')).toHaveTextContent('OK')
    expect(row('vps-b')).toHaveTextContent('STALE')
    expect(row('vps-c')).toHaveTextContent('OFFLINE')
    expect(row('vps-d')).toHaveTextContent('NEW')
  })

  it('shows current values, keeps a measured zero and marks unknown as —', async () => {
    await mount()
    expect(row('vps-a')).toHaveTextContent('CPU37%')
    expect(row('vps-a')).toHaveTextContent('DISK /data88%') // the fullest mount represents the host
    expect(row('vps-a')).toHaveTextContent('up 14d 06h')
    expect(row('vps-b')).toHaveTextContent('CPU0%') // zero is a value
    expect(row('vps-c')).toHaveTextContent('CPU—') // an hour-old sample is unknown, never "current"
    expect(row('vps-d')).toHaveTextContent('never seen')
    expect(row('vps-d')).toHaveTextContent('CPU—')
  })

  it('shows how long ago a host was seen', async () => {
    await mount()
    expect(row('vps-a')).toHaveTextContent(/just now|\ds ago/)
    expect(row('vps-b')).toHaveTextContent('3m ago')
    expect(row('vps-c')).toHaveTextContent('1h ago')
  })

  it('expands a row in place, one at a time, and collapses it again', async () => {
    const user = userEvent.setup()
    await mount()
    expect(row('vps-a')).toHaveAttribute('aria-expanded', 'false')
    await user.click(row('vps-a'))
    expect(row('vps-a')).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('heading', { name: 'Disks' })).toBeInTheDocument()

    await user.click(row('vps-b'))
    await waitFor(() => expect(row('vps-a')).toHaveAttribute('aria-expanded', 'false'))
    expect(row('vps-b')).toHaveAttribute('aria-expanded', 'true')

    await user.click(row('vps-b'))
    await waitFor(() => expect(row('vps-b')).toHaveAttribute('aria-expanded', 'false'))
  })

  it('opens and closes with the keyboard', async () => {
    const user = userEvent.setup()
    await mount()
    await user.tab() // monitoring tab
    await user.tab() // analytics tab
    await user.tab() // guide tab
    await user.tab() // add host
    await user.tab() // logout
    await user.tab() // summary line "?"
    await user.tab() // STATUS "?"
    await user.tab() // HOSTS "?"
    await user.tab() // first row
    expect(row('vps-a')).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(row('vps-a')).toHaveAttribute('aria-expanded', 'true')
    // Base UI 1.8: rows are ordinary tab stops; the open row's "?" buttons come before the next row.
    await user.keyboard('{Enter}')
    await user.tab()
    expect(row('vps-b')).toHaveFocus()
    await user.keyboard(' ')
    await waitFor(() => expect(row('vps-b')).toHaveAttribute('aria-expanded', 'true'))
  })
})

describe('host details', () => {
  it('lists disks with a meter and sizes, checks, interfaces and events for the host', async () => {
    const user = userEvent.setup()
    await mount()
    await user.click(row('vps-a'))
    const disks = await screen.findByRole('region', { name: 'Disks' })
    expect(within(disks).getByText('/data')).toBeInTheDocument()
    expect(within(disks).getByText(/8\.0 GB \/ 10\.0 GB/)).toBeInTheDocument()
    const meter = within(disks).getByRole('meter', { name: '/data used' })
    expect(meter).toHaveAttribute('aria-valuenow', '88')
    expect(screen.getByRole('region', { name: 'Checks' })).toHaveTextContent('disk.root')
    expect(screen.getByRole('region', { name: 'Checks' })).toHaveTextContent('OK')
    expect(screen.getByRole('region', { name: 'Host events' })).toHaveTextContent('ban 203.0.113.7')
    expect(screen.getByRole('figure', { name: 'CPU usage, last hour' })).toBeInTheDocument()
  })

  it('says plainly when a host has no disks, checks or events', async () => {
    const user = userEvent.setup()
    await mount()
    await user.click(row('vps-d'))
    expect(await screen.findByText('no disk data')).toBeInTheDocument()
    expect(screen.getByText('no checks reported')).toBeInTheDocument()
    expect(screen.getByText('no events')).toBeInTheDocument()
    expect(screen.getByText('no interface data')).toBeInTheDocument()
  })

  it('degrades to the overview data when the history request fails', async () => {
    const user = userEvent.setup()
    await mount(FULL, {})
    serve(FULL, {
      '/api/metrics?host=': () => json({ error: 'db busy' }, 500),
    })
    await user.click(row('vps-a'))
    expect(await screen.findByText(/Could not load the full history \(db busy\)/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Disks' })).toBeInTheDocument()
  })
})

describe('EVENTS', () => {
  it('lists events newest first with the level as text and the host by name', async () => {
    await mount()
    const events = screen.getByRole('region', { name: 'Events' })
    const items = within(events).getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(items[0]).toHaveTextContent('WARN')
    expect(items[0]).toHaveTextContent('vps-a')
    expect(items[0]).toHaveTextContent('ban 203.0.113.7')
    expect(items[1]).toHaveTextContent('ERROR')
    expect(items[1]).toHaveTextContent('vps-c')
  })

  it('shows a live event that arrives through the stream', async () => {
    const { store } = await mount()
    store.handleFrame({
      type: 'event',
      host_id: 'b',
      record: event('b', 0, 'critical', 'disk full'),
    })
    const events = screen.getByRole('region', { name: 'Events' })
    expect(await within(events).findByText('disk full')).toBeInTheDocument()
    expect(within(events).getAllByRole('listitem')[0]).toHaveTextContent('CRITICAL')
  })
})

describe('live values', () => {
  it('updates a row when a metric frame arrives and marks the host seen', async () => {
    const { store } = await mount()
    expect(row('vps-d')).toHaveTextContent('NEW')
    store.handleFrame({
      type: 'metric',
      host_id: 'd',
      record: metric('d', 'cpu.usage_percent', 0, 12),
    })
    await waitFor(() => expect(row('vps-d')).toHaveTextContent('CPU12%'))
    expect(row('vps-d')).toHaveTextContent('OK')
  })
})

describe('command bar', () => {
  it('logs out', async () => {
    const { onLogout } = await mount()
    await userEvent.setup().click(screen.getByRole('button', { name: 'logout' }))
    expect(onLogout).toHaveBeenCalled()
  })
})

describe('accessibility', () => {
  it('has no axe violations in the ledger, expanded details and events', async () => {
    const user = userEvent.setup()
    const { container } = await mount()
    expect(await axe(container)).toHaveNoViolations()
    await user.click(row('vps-a'))
    await screen.findByRole('heading', { name: 'Disks' })
    expect(await axe(container)).toHaveNoViolations()
  })

  it('has no axe violations in the empty and error states', async () => {
    const { container } = await mount({ hosts: [], latest: [], events: [] })
    expect(await axe(container)).toHaveNoViolations()
  })
})
