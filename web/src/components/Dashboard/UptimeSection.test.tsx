import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { DashboardStore } from '../../data/store'
import type { UptimeResultDTO, UptimeTargetDTO } from '../../domain/types'
import { Dashboard } from './Dashboard'
import { targetErrorMessage } from './AddTargetDialog'
import { ApiError } from '../../api/client'

const NOW = Date.now()
const iso = (offsetMs: number) => new Date(NOW + offsetMs).toISOString()
const fetchMock = vi.fn<typeof fetch>()

const result = (id: string, offsetMs: number, ok: boolean, latency: number | null, extra: Partial<UptimeResultDTO> = {}): UptimeResultDTO => ({
  target_id: id, ts: iso(offsetMs), ok, latency_ms: latency, status_code: ok ? 200 : null, error: ok ? '' : 'connection refused', cert_expires_at: null, ...extra,
})
const target = (id: string, name: string, over: Partial<UptimeTargetDTO> = {}): UptimeTargetDTO => ({
  id, name, kind: 'http', target: `https://${name}.example`, interval_seconds: 60, created_at: iso(-86_400_000), last: null, latency: [], ...over,
})

let targets: UptimeTargetDTO[]
let created: unknown[]
let removed: string[]
let postStatus: number
let postBody: unknown
let deleteStatus: number

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })

beforeEach(() => {
  created = []
  removed = []
  postStatus = 201
  postBody = { id: 'new' }
  deleteStatus = 204
  targets = [
    target('a', 'api', { last: result('a', -20_000, true, 142, { cert_expires_at: iso(9 * 86_400_000 + 3600_000) }), latency: [[NOW - 60_000, 130], [NOW - 20_000, 142]] }),
    target('b', 'db', { kind: 'tcp', target: 'db.internal:5432', last: result('b', -20_000, false, null), latency: [[NOW - 60_000, 5], [NOW - 20_000, null]] }),
    target('c', 'old', { last: result('c', -3600_000, true, 80) }),
    target('d', 'fresh'),
  ]
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/uptime' && init?.method === 'POST') {
      created.push(JSON.parse(String(init.body)))
      return json(postBody, postStatus)
    }
    if (url.startsWith('/api/uptime/') && init?.method === 'DELETE') {
      removed.push(decodeURIComponent(url.split('/').pop()!))
      return deleteStatus === 204 ? new Response(null, { status: 204 }) : json({ error: 'target not found' }, deleteStatus)
    }
    if (url === '/api/uptime') return json({ targets })
    if (url.startsWith('/api/hosts')) return json({ hosts: [] })
    if (url.startsWith('/api/metrics')) return json({ metrics: [] })
    if (url.startsWith('/api/events')) return json({ events: [] })
    if (url.startsWith('/api/checks')) return json({ checks: [] })
    throw new Error(`unexpected ${url}`)
  })
})
afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

async function mount() {
  const store = new DashboardStore()
  await store.load()
  const view = render(<Dashboard store={store} onLogout={vi.fn()} />)
  return { store, ...view }
}

const section = () => screen.getByRole('region', { name: 'Uptime' })
const rowOf = (name: string) => within(section()).getByText(name).closest('li')!

describe('UPTIME ledger', () => {
  it('shows the state of each target in text', async () => {
    await mount()
    expect(rowOf('api')).toHaveTextContent('UP')
    expect(rowOf('db')).toHaveTextContent('DOWN')
    expect(rowOf('old')).toHaveTextContent('STALE') // its last result is an hour old
    expect(rowOf('fresh')).toHaveTextContent('NEW')
    expect(rowOf('fresh')).toHaveTextContent('no result yet')
  })

  it('shows latency and the certificate days, and — when there is no measurement', async () => {
    await mount()
    expect(rowOf('api')).toHaveTextContent('142 ms')
    expect(rowOf('api')).toHaveTextContent('TLS 9d')
    expect(rowOf('api')).toHaveTextContent('https://api.example')
    expect(rowOf('db')).toHaveTextContent('LATENCY—') // a down target has no latency, never "0 ms"
    expect(rowOf('db')).toHaveTextContent('tcp db.internal:5432')
    expect(rowOf('fresh')).toHaveTextContent('LATENCY—')
  })

  it('explains a DOWN target with the probe error', async () => {
    await mount()
    expect(rowOf('db')).toHaveTextContent('connection refused')
  })

  it('warns in text about a certificate that is about to expire or has expired', async () => {
    targets = [
      target('x', 'soon', { last: result('x', -10_000, true, 50, { cert_expires_at: iso(5 * 86_400_000) }) }),
      target('y', 'lapsed', { kind: 'tls', target: 'a.example:443', last: result('y', -10_000, false, null, { cert_expires_at: iso(-2 * 86_400_000) }) }),
    ]
    await mount()
    expect(rowOf('soon')).toHaveTextContent(/TLS [45]d/)
    expect(rowOf('lapsed')).toHaveTextContent('TLS expired')
  })

  it('teaches how to start when there are no targets', async () => {
    targets = []
    await mount()
    expect(within(section()).getByText(/No targets yet/)).toBeInTheDocument()
    expect(within(section()).getByRole('button', { name: '+ add target' })).toBeInTheDocument()
  })

  it('updates a row live when a probe result arrives', async () => {
    const { store } = await mount()
    store.handleFrame({ type: 'uptime', target_id: 'b', record: result('b', 0, true, 33) })
    await waitFor(() => expect(rowOf('db')).toHaveTextContent('UP'))
    expect(rowOf('db')).toHaveTextContent('33 ms')
  })

  it('counts up and down probes in the STATUS strip, leaving unknown ones out of both', async () => {
    await mount()
    const strip = screen.getByRole('region', { name: 'Status' })
    const value = (label: string) => within(strip).getByText(label, { selector: 'dt' }).nextElementSibling!.textContent
    expect(value('probes up')).toBe('1')
    expect(value('probes down')).toBe('1') // STALE and NEW count in neither
  })

  it('has no probe counters when there are no targets', async () => {
    targets = []
    await mount()
    expect(within(screen.getByRole('region', { name: 'Status' })).queryByText('probes up')).not.toBeInTheDocument()
  })
})

describe('removing a target', () => {
  it('asks inline first, and cancel changes nothing', async () => {
    const user = userEvent.setup()
    await mount()
    await user.click(within(rowOf('api')).getByRole('button', { name: 'Remove api' }))
    expect(within(rowOf('api')).getByRole('group', { name: 'Confirm removing api' })).toBeInTheDocument()
    await user.click(within(rowOf('api')).getByRole('button', { name: 'cancel' }))
    expect(within(rowOf('api')).getByRole('button', { name: 'Remove api' })).toBeInTheDocument()
    expect(removed).toEqual([])
  })

  it('deletes on confirmation and the ledger reloads', async () => {
    const user = userEvent.setup()
    await mount()
    await user.click(within(rowOf('db')).getByRole('button', { name: 'Remove db' }))
    targets = targets.filter((t) => t.id !== 'b') // what the server returns after the delete
    await user.click(within(rowOf('db')).getByRole('button', { name: 'remove target and its history' }))
    expect(removed).toEqual(['b'])
    await waitFor(() => expect(within(section()).queryByText('db')).not.toBeInTheDocument())
  })

  it('shows the failure and keeps the row when the server refuses', async () => {
    deleteStatus = 500
    const user = userEvent.setup()
    await mount()
    await user.click(within(rowOf('api')).getByRole('button', { name: 'Remove api' }))
    await user.click(within(rowOf('api')).getByRole('button', { name: 'remove target and its history' }))
    expect(await within(rowOf('api')).findByRole('alert')).toHaveTextContent('target not found')
    expect(within(section()).getByText('api')).toBeInTheDocument()
  })
})

describe('add target dialog', () => {
  async function open() {
    const user = userEvent.setup()
    const view = await mount()
    await user.click(within(section()).getByRole('button', { name: '+ add target' }))
    const dialog = await screen.findByRole('dialog', { name: 'Add uptime target' })
    return { user, dialog, ...view }
  }

  it('is accessible, focuses the name and offers the three kinds', async () => {
    const { dialog } = await open()
    expect(await axe(dialog)).toHaveNoViolations()
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveFocus())
    const group = screen.getByRole('radiogroup', { name: 'Type' })
    expect(within(group).getAllByRole('radio').map((r) => r.closest('label')?.textContent)).toEqual(['HTTP', 'TCP', 'TLS'])
    expect(within(group).getByRole('radio', { name: 'HTTP' })).toBeChecked()
  })

  it('creates an http target with the entered values', async () => {
    const { user } = await open()
    await user.type(screen.getByLabelText('Name'), 'shop')
    await user.type(screen.getByLabelText('URL'), 'https://shop.example/health')
    await user.click(screen.getByRole('button', { name: 'Add target' }))
    await waitFor(() => expect(created).toEqual([{ name: 'shop', kind: 'http', target: 'https://shop.example/health', interval_seconds: 60 }]))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('switches the field wording and hint with the kind and sends the chosen kind and interval', async () => {
    const { user } = await open()
    await user.click(screen.getByRole('radio', { name: 'TLS' }))
    expect(screen.getByLabelText('Host and port')).toBeInTheDocument()
    expect(screen.getByText(/days left on the certificate/)).toBeInTheDocument()
    await user.type(screen.getByLabelText('Name'), 'mail')
    await user.type(screen.getByLabelText('Host and port'), 'mail.example:465')
    const interval = screen.getByLabelText('Interval, seconds')
    await user.clear(interval)
    await user.type(interval, '300')
    await user.click(screen.getByRole('button', { name: 'Add target' }))
    await waitFor(() => expect(created).toEqual([{ name: 'mail', kind: 'tls', target: 'mail.example:465', interval_seconds: 300 }]))
  })

  it('shows the server explanation for a rejected target and can retry', async () => {
    postStatus = 400
    postBody = { error: 'target must be an absolute http(s) URL' }
    const { user } = await open()
    await user.type(screen.getByLabelText('Name'), 'bad')
    await user.type(screen.getByLabelText('URL'), 'example.com')
    await user.click(screen.getByRole('button', { name: 'Add target' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Target must be an absolute http(s) URL.')
    expect(screen.getByLabelText('URL')).toHaveAttribute('aria-invalid', 'true')
    postStatus = 201
    postBody = { id: 'ok' }
    await user.click(screen.getByRole('button', { name: 'Add target' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('does not send an empty form and closes with Escape', async () => {
    const { user } = await open()
    await user.click(screen.getByRole('button', { name: 'Add target' }))
    expect(created).toEqual([])
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('resets to HTTP after closing', async () => {
    const { user } = await open()
    await user.click(screen.getByRole('radio', { name: 'TCP' }))
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await user.click(within(section()).getByRole('button', { name: '+ add target' }))
    await screen.findByRole('dialog')
    expect(screen.getByRole('radio', { name: 'HTTP' })).toBeChecked()
  })
})

describe('accessibility and messages', () => {
  it('has no axe violations with targets in every state', async () => {
    const { container } = await mount()
    expect(await axe(container)).toHaveNoViolations()
  })

  it('maps API failures to operator text', () => {
    expect(targetErrorMessage(new ApiError('http', 409, 'a target with this name already exists'))).toBe('A target with this name already exists.')
    expect(targetErrorMessage(new ApiError('http', 409, 'target limit reached (50)'))).toBe('Target limit reached (50).')
    expect(targetErrorMessage(new ApiError('network', 0, 'x'))).toContain('Cannot reach the server')
    expect(targetErrorMessage(new ApiError('http', 500, 'x'))).toContain('(500)')
    expect(targetErrorMessage(new Error('boom'))).toBe('Could not add the target. Try again.')
  })
})
