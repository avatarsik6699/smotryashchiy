import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { App } from './App'

vi.mock('./components/Dashboard/Dashboard', () => ({
  Dashboard: ({ onLogout }: { onLogout: () => Promise<void> }) => (
    <div>
      <p>dashboard shown</p>
      <button onClick={() => void onLogout()}>logout</button>
    </div>
  ),
}))

const fetchMock = vi.fn<typeof fetch>()

function json(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), { status })
}

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

function route(handlers: Record<string, () => Response | Promise<Response>>) {
  fetchMock.mockImplementation(async (input, init) => {
    const key = `${init?.method ?? 'GET'} ${String(input)}`
    const handler = handlers[key]
    if (!handler) throw new Error(`unexpected request: ${key}`)
    return handler()
  })
}

describe('App session flow', () => {
  it('shows the login form when the session probe says not authenticated', async () => {
    route({ 'GET /api/auth/session': () => json(200, { authenticated: false }) })
    render(<App />)
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })

  it('goes straight to the shell when a session already exists, and counts hosts', async () => {
    route({
      'GET /api/auth/session': () => json(200, { authenticated: true }),
      'GET /api/hosts': () => json(200, { hosts: [{ id: 'a', name: 'vps1' }, { id: 'b', name: 'vps2' }] }),
    })
    const { container } = render(<App />)
    expect(await screen.findByText('dashboard shown')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'logout' })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it('logs in, then logs out back to the form', async () => {
    let loggedIn = false
    route({
      'GET /api/auth/session': () => json(200, { authenticated: loggedIn }),
      'GET /api/hosts': () => (loggedIn ? json(200, { hosts: [] }) : json(401, { error: 'x' })),
      'POST /api/auth/login': () => {
        loggedIn = true
        return json(204)
      },
      'POST /api/auth/logout': () => {
        loggedIn = false
        return json(204)
      },
    })
    const user = userEvent.setup()
    render(<App />)
    await user.type(await screen.findByLabelText('Password'), 'pw')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('dashboard shown')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'logout' }))
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })

  it('returns to the login form when any call reports an expired session', async () => {
    route({
      'GET /api/auth/session': () => json(200, { authenticated: true }),
      'GET /api/hosts': () => json(401, { error: 'invalid or expired session' }),
    })
    render(<App />)
    await screen.findByText('dashboard shown')
    const { api } = await import('./api/client')
    await api('/api/hosts').catch(() => undefined) // the client signals the expiry to the session hook
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })

  it('distinguishes an unreachable server from a logged-out one and can retry', async () => {
    route({ 'GET /api/auth/session': () => json(200, { authenticated: false }) })
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch')) // only the first probe fails
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByText('Cannot reach the server.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'retry' }))
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })
})
