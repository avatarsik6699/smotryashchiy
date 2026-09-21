import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { App } from './App'

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
    expect(await screen.findByText('2 hosts registered')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'logout' })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it('uses the singular for one host and says so plainly for none', async () => {
    route({
      'GET /api/auth/session': () => json(200, { authenticated: true }),
      'GET /api/hosts': () => json(200, { hosts: [{ id: 'a', name: 'vps1' }] }),
    })
    const first = render(<App />)
    expect(await screen.findByText('1 host registered')).toBeInTheDocument()
    first.unmount()
    route({
      'GET /api/auth/session': () => json(200, { authenticated: true }),
      'GET /api/hosts': () => json(200, { hosts: [] }),
    })
    render(<App />)
    expect(await screen.findByText('0 hosts registered')).toBeInTheDocument()
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
    expect(await screen.findByText('0 hosts registered')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'logout' }))
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })

  it('returns to the login form when the session expires mid-use', async () => {
    let calls = 0
    route({
      'GET /api/auth/session': () => json(200, { authenticated: true }),
      'GET /api/hosts': () => {
        calls += 1
        // 1: the shell's own load; the session then expires.
        return calls <= 1 ? json(200, { hosts: [] }) : json(401, { error: 'invalid or expired session' })
      },
    })
    render(<App />)
    await screen.findByText('0 hosts registered')
    // The shell's own request is refused (session expired): the client signals it and the app shows the form.
    const { api } = await import('./api/client')
    await api('/api/hosts').catch(() => undefined)
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
