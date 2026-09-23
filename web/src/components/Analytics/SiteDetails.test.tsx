import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { SiteDetails } from './SiteDetails'

const site = { id: 's1', name: 'Infraege', domain: 'infraege.ru', created_at: new Date().toISOString() }

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ pageviews: 3, visitors: 2, top_pages: [{ path: '/', count: 3 }], top_referrers: [] }), { status: 200 }),
    ),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('SiteDetails', () => {
  it('says the ranges are UTC days and ties the note to the range toggle', async () => {
    const { container } = render(<SiteDetails site={site} />)
    await screen.findByText('Pageviews')
    const note = screen.getByText('days in UTC')
    expect(screen.getByRole('group', { name: 'Time range' })).toHaveAttribute('aria-describedby', note.id)
    expect(await axe(container)).toHaveNoViolations()
  })
})
