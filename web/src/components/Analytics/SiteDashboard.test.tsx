import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import type { AnalyticsQuery } from '../../domain/analytics'
import { SiteDashboard } from './SiteDashboard'

const sites = [{ id: 's1', name: 'Кодумка', domain: 'kodumka.ru', created_at: '2026-10-01T00:00:00Z' }]
const fetchMock = vi.fn<typeof fetch>()
const bodies: AnalyticsQuery[] = []

const json = (v: unknown, init?: ResponseInit) => new Response(JSON.stringify(v), { status: 200, ...init })

function answer(q: AnalyticsQuery) {
  const base = { date_range: ['2026-09-27', '2026-10-26'], compare_range: q.compare ? ['2026-08-28', '2026-09-26'] : undefined, timezone: 'Europe/Moscow', source: 'raw' }
  if (q.dimension?.startsWith('time:')) {
    return { ...base, rows: [0, 1, 2].map((i) => ({ value: `2026-10-2${i}`, metrics: { visitors: 10 + i }, compare: q.compare ? { visitors: 8 } : undefined, compare_value: `2026-09-2${i}` })) }
  }
  if (q.dimension === 'channel') {
    return { ...base, rows: [{ value: 'organic_search', metrics: { visitors: 70 } }, { value: 'direct', metrics: { visitors: 30 } }] }
  }
  if (q.dimension) return { ...base, rows: [] }
  return {
    ...base,
    rows: [
      {
        value: '',
        metrics: { visitors: 118, visits: 140, pageviews: 400, views_per_visit: 2.9, bounce_rate: 46, visit_duration: 230 },
        compare: { visitors: 100, visits: 140, pageviews: 300, views_per_visit: 2.7, bounce_rate: 49, visit_duration: 250 },
      },
    ],
  }
}

beforeEach(() => {
  bodies.length = 0
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.endsWith('/realtime')) return json({ visitors: 7 })
    const q = JSON.parse(String(init?.body)) as AnalyticsQuery
    bodies.push(q)
    if (q.format === 'csv') return new Response('channel,visitors\n', { status: 200, headers: { 'Content-Type': 'text/csv' } })
    return json(answer(q))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SiteDashboard', () => {
  it('shows the figures with deltas coloured by better or worse', async () => {
    const { container } = render(<SiteDashboard sites={sites} />)
    const kpis = screen.getByRole('region', { name: 'Key figures' })
    await within(kpis).findByText('118')
    expect(within(kpis).getByText('Visitor-days')).toBeInTheDocument()
    const visitors = within(kpis).getByRole('button', { name: /Visitor-days/ })
    expect(visitors).toHaveAttribute('aria-pressed', 'true')
    expect(visitors).toHaveTextContent('+18% vs previous, better')
    expect(within(kpis).getByRole('button', { name: /Bounce rate/ })).toHaveTextContent('−3 pp vs previous, better')
    expect(within(kpis).getByRole('button', { name: /Visit duration/ })).toHaveTextContent('−8% vs previous, worse')
    expect(await screen.findByText('7')).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it('filters every query by a clicked row and removes the filter', async () => {
    const user = userEvent.setup()
    render(<SiteDashboard sites={sites} />)
    const sources = screen.getByRole('region', { name: 'Sources' })
    await user.click(await within(sources).findByRole('button', { name: /Organic search/ }))
    const chip = await screen.findByRole('button', { name: 'Remove filter channel is Organic search' })
    await waitFor(() => expect(bodies.some((b) => !b.dimension && b.filters?.[0]?.values[0] === 'organic_search')).toBe(true))
    expect(bodies.some((b) => b.dimension === 'page' && b.filters?.[0]?.dimension === 'channel')).toBe(true)
    await user.click(chip)
    expect(screen.queryByRole('button', { name: /Remove filter/ })).not.toBeInTheDocument()
  })

  it('moves the chart tooltip with the arrow keys', async () => {
    const user = userEvent.setup()
    render(<SiteDashboard sites={sites} />)
    const chart = await screen.findByRole('img', { name: /Visitor-days per day, last 30 days/ })
    await waitFor(() => expect(screen.getAllByRole('row').length).toBeGreaterThan(3))
    chart.focus()
    expect(await screen.findByText('Thu 22 Oct')).toBeInTheDocument()
    await user.keyboard('{ArrowLeft}')
    expect(screen.getByRole('status')).toHaveTextContent('Wed 21 Oct')
    expect(screen.getByRole('status')).toHaveTextContent('11 visitor-days')
    expect(screen.getByRole('status')).toHaveTextContent('8 previous (Mon 21 Sep)')
  })

  it('downloads a panel as CSV from its details', async () => {
    const user = userEvent.setup()
    const createObjectURL = vi.fn(() => 'blob:x')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    render(<SiteDashboard sites={sites} />)
    const sources = screen.getByRole('region', { name: 'Sources' })
    await user.click(within(sources).getByRole('button', { name: 'details' }))
    const dialog = await screen.findByRole('dialog', { name: 'Sources: channel' })
    await within(dialog).findByText('Organic search')
    await user.click(within(dialog).getByRole('button', { name: 'Download CSV' }))
    await waitFor(() => expect(click).toHaveBeenCalled())
    expect(bodies.at(-1)).toMatchObject({ dimension: 'channel', format: 'csv', limit: 100 })
    click.mockRestore()
  })
})
