import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Dashboard } from '../Dashboard/Dashboard'
import { DashboardStore } from '../../data/store'
import { createErrorMessage, trackingSnippet } from './AddSiteDialog'
import { ApiError } from '../../api/client'

const fetchMock = vi.fn<typeof fetch>()

const created = {
  id: 's1',
  name: 'Infraege',
  domain: 'infraege.ru',
  created_at: new Date().toISOString(),
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status })
}

let postResponse: () => Response | Promise<Response>
let sitesListed: {
  id: string
  name: string
  domain: string
  created_at: string
}[]

beforeEach(() => {
  postResponse = () => json(created, 201)
  sitesListed = []
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/sites' && init?.method === 'POST') return postResponse()
    if (url === '/api/sites') return json({ sites: sitesListed })
    if (url.startsWith('/api/sites/') && url.includes('/stats'))
      return json({
        pageviews: 0,
        visitors: 0,
        top_pages: [],
        top_referrers: [],
      })
    if (url.startsWith('/api/hosts')) return json({ hosts: [] })
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

async function openAnalyticsAddDialog() {
  const store = new DashboardStore()
  await store.load()
  render(<Dashboard store={store} onLogout={vi.fn()} />)
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'analytics' }))
  await user.click(screen.getByRole('button', { name: '+ add site' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add site' })
  return { user, dialog }
}

describe('Analytics tab', () => {
  it('switches from Monitoring to Analytics and shows the empty-sites notice', async () => {
    const store = new DashboardStore()
    await store.load()
    const user = userEvent.setup()
    render(<Dashboard store={store} onLogout={vi.fn()} />)
    expect(screen.getByRole('heading', { name: 'Hosts' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'analytics' }))
    expect(await screen.findByText(/No sites yet/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Hosts' })).not.toBeInTheDocument()
  })
})

describe('AddSiteDialog', () => {
  it('creates a site and shows its tracking snippet', async () => {
    const { user, dialog } = await openAnalyticsAddDialog()
    await user.type(screen.getByLabelText('Site name'), 'Infraege')
    await user.type(screen.getByLabelText('Domain'), 'infraege.ru')
    await user.click(screen.getByRole('button', { name: 'Create site' }))

    expect(await screen.findByLabelText('Tracking snippet')).toHaveTextContent('data-site="s1"')
    expect(dialog).toHaveTextContent('infraege.ru')
    expect(dialog).toHaveTextContent('Content-Security-Policy')
  })

  it('reloads the site list after creation', async () => {
    const { user } = await openAnalyticsAddDialog()
    await user.type(screen.getByLabelText('Site name'), 'Infraege')
    await user.type(screen.getByLabelText('Domain'), 'infraege.ru')
    sitesListed = [created] // the next GET /api/sites (triggered by onCreated) returns the new site
    await user.click(screen.getByRole('button', { name: 'Create site' }))
    await screen.findByLabelText('Tracking snippet')
    await user.click(screen.getByRole('button', { name: 'Done' }))
    expect(await screen.findByText('Infraege')).toBeInTheDocument()
  })

  it('shows a conflict message for a duplicate domain', async () => {
    postResponse = () => json({ error: 'a site with this domain already exists' }, 409)
    const { user } = await openAnalyticsAddDialog()
    await user.type(screen.getByLabelText('Site name'), 'Infraege')
    await user.type(screen.getByLabelText('Domain'), 'infraege.ru')
    await user.click(screen.getByRole('button', { name: 'Create site' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/already exists/))
  })
})

describe('createErrorMessage', () => {
  it('maps a 409 to a duplicate-domain message', () => {
    expect(createErrorMessage(new ApiError('http', 409, 'conflict'))).toMatch(/already exists/)
  })
  it('maps a network error to an unreachable message', () => {
    expect(createErrorMessage(new ApiError('network', 0, 'x'))).toMatch(/Cannot reach/)
  })
})

describe('trackingSnippet', () => {
  it('embeds the site id and this origin', () => {
    const snippet = trackingSnippet({ id: 'abc123' })
    expect(snippet).toContain('data-site="abc123"')
    expect(snippet).toContain('/track.js')
  })
})
