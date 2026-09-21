import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { ApiError } from '../../api/client'
import { DashboardStore } from '../../data/store'
import { Dashboard } from './Dashboard'
import { createErrorMessage, enrollCommand } from './AddHostDialog'

const fetchMock = vi.fn<typeof fetch>()
const NOW = Date.now()

const created = {
  host: { id: 'h1', name: 'vps-new', created_at: new Date(NOW).toISOString(), last_seen_at: null },
  server_url: 'https://monitor.example.com',
  secret: 'S3cr3t-value',
  expires_at: new Date(NOW + 3600_000).toISOString(),
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status })
}

let postResponse: () => Response | Promise<Response>
let hostsCalls = 0

beforeEach(() => {
  hostsCalls = 0
  postResponse = () => json(created, 201)
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/hosts' && init?.method === 'POST') return postResponse()
    if (url.startsWith('/api/hosts')) {
      hostsCalls += 1
      return json({ hosts: [] })
    }
    if (url.startsWith('/api/metrics')) return json({ metrics: [] })
    if (url.startsWith('/api/events')) return json({ events: [] })
    if (url.startsWith('/api/checks')) return json({ checks: [] })
    if (url.startsWith('/api/uptime')) return json({ targets: [] })
    throw new Error(`unexpected ${url}`)
  })
})
afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

async function openDialog() {
  const store = new DashboardStore()
  await store.load()
  render(<Dashboard store={store} onLogout={vi.fn()} />)
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: '+ add host' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add host' })
  return { user, dialog, store }
}

describe('AddHostDialog', () => {
  it('opens as an accessible modal dialog with the name field focused', async () => {
    const { dialog } = await openDialog()
    expect(await axe(dialog)).toHaveNoViolations()
    const input = screen.getByLabelText('Host name')
    expect(input).toBeRequired()
    await waitFor(() => expect(input).toHaveFocus())
  })

  it('creates the host, shows the one-time command with its expiry and refreshes the ledger', async () => {
    const { user } = await openDialog()
    const before = hostsCalls
    await user.type(screen.getByLabelText('Host name'), 'vps-new')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    const command = await screen.findByLabelText('Enrollment command')
    expect(command).toHaveTextContent('smotryashchiy agent enroll --server https://monitor.example.com --secret S3cr3t-value')
    expect(screen.getByText(/shown once and expires at/)).toBeInTheDocument()
    expect(screen.getByText('vps-new')).toBeInTheDocument()
    const post = fetchMock.mock.calls.find((c) => c[1]?.method === 'POST')!
    expect(JSON.parse(String(post[1]!.body))).toEqual({ name: 'vps-new' })
    await waitFor(() => expect(hostsCalls).toBeGreaterThan(before)) // the new host is fetched
  })

  it('copies the command and announces it politely', async () => {
    const { user } = await openDialog()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await user.type(screen.getByLabelText('Host name'), 'vps-new')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    await screen.findByLabelText('Enrollment command')
    await user.click(screen.getByRole('button', { name: 'Copy command' }))
    expect(writeText).toHaveBeenCalledWith(enrollCommand(created))
    expect(await screen.findByText('Copied to the clipboard.')).toBeInTheDocument()
  })

  it('tells the operator to copy by hand when the clipboard is unavailable', async () => {
    const { user } = await openDialog()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) }, configurable: true })
    await user.type(screen.getByLabelText('Host name'), 'vps-new')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    await user.click(await screen.findByRole('button', { name: 'Copy command' }))
    expect(await screen.findByText('Could not copy. Select the command and copy it manually.')).toBeInTheDocument()
  })

  it('shows a specific error for a duplicate name and lets the operator retry', async () => {
    postResponse = () => json({ error: 'host name already exists' }, 409)
    const { user } = await openDialog()
    await user.type(screen.getByLabelText('Host name'), 'web-1')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A host with this name already exists.')
    expect(screen.getByLabelText('Host name')).toHaveAttribute('aria-invalid', 'true')
    postResponse = () => json(created, 201)
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    expect(await screen.findByLabelText('Enrollment command')).toBeInTheDocument()
  })

  it('does not send an empty name', async () => {
    const { user } = await openDialog()
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === 'POST')).toBe(false)
  })

  it('forgets the secret when the dialog closes', async () => {
    const { user } = await openDialog()
    await user.type(screen.getByLabelText('Host name'), 'vps-new')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    await screen.findByLabelText('Enrollment command')
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '+ add host' }))
    await screen.findByRole('dialog', { name: 'Add host' })
    expect(screen.queryByText(/S3cr3t-value/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Host name')).toHaveValue('')
  })

  it('closes with Escape', async () => {
    const { user } = await openDialog()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('disables the button while the request is pending', async () => {
    let finish: (r: Response) => void = () => {}
    postResponse = () => new Promise<Response>((resolve) => (finish = resolve))
    const { user } = await openDialog()
    await user.type(screen.getByLabelText('Host name'), 'vps-new')
    await user.click(screen.getByRole('button', { name: 'Create host' }))
    expect(await screen.findByRole('button', { name: 'Creating…' })).toHaveAttribute('aria-disabled', 'true')
    finish(json(created, 201))
    await screen.findByLabelText('Enrollment command')
  })
})

describe('createErrorMessage', () => {
  it('distinguishes the failure modes', () => {
    expect(createErrorMessage(new ApiError('http', 409, 'x'))).toBe('A host with this name already exists.')
    expect(createErrorMessage(new ApiError('http', 400, 'host name must contain 1..80 bytes'))).toBe('Invalid name: host name must contain 1..80 bytes.')
    expect(createErrorMessage(new ApiError('network', 0, 'x'))).toContain('Cannot reach the server')
    expect(createErrorMessage(new ApiError('rate_limited', 429, 'x'))).toContain('Too many requests')
    expect(createErrorMessage(new ApiError('http', 500, 'x'))).toContain('(500)')
    expect(createErrorMessage(new Error('boom'))).toBe('Could not create the host. Try again.')
  })
  it('builds the command an agent needs', () => {
    expect(enrollCommand({ server_url: 'http://a:8080', secret: 'abc' })).toBe('smotryashchiy agent enroll --server http://a:8080 --secret abc')
  })
})
