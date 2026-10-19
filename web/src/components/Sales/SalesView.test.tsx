import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { formatRubles } from '../../domain/sales'
import { sampleDocument, sampleSummary } from '../../domain/sales.fixture'
import { SalesView } from './SalesView'

const now = new Date('2026-10-19T09:30:00Z').getTime()

describe('SalesView (Change 27)', () => {
  it('shows the summary, its freshness and the day ledger', async () => {
    const { container } = render(<SalesView status="ready" error={null} documents={[sampleDocument()]} now={now} onRetry={() => {}} />)
    expect(screen.getByRole('heading', { name: 'Sales' })).toBeInTheDocument()
    expect(screen.getByText(/infraege-prod · summary 30m ago/)).toBeInTheDocument()
    const measures = container.querySelector('dl')!
    expect(within(measures).getByText('Revenue 30d')).toBeInTheDocument()
    expect([...measures.querySelectorAll('dd')].map((dd) => dd.textContent)).toContain(formatRubles(417_000))
    expect(screen.getByRole('meter', { name: 'НПД limit used, 2026' })).toHaveAttribute('aria-valuenow', '75')
    expect(screen.getByText('No payment problems.')).toBeInTheDocument()
    expect(screen.getAllByRole('row')).toHaveLength(1 + 30 + 1)
    fireEvent.click(screen.getByRole('button', { name: '7d' }))
    expect(screen.getAllByRole('row')).toHaveLength(1 + 7 + 1)
    expect(await axe(container)).toHaveNoViolations()
  })

  it('marks a stale summary and lists problems', () => {
    const body = sampleSummary({ problems: { stuck_orders: 1, refunds_requested: 0, refunds_pending: 0, refunds_failed: 0 } })
    render(<SalesView status="ready" error={null} documents={[sampleDocument(body)]} now={now + 3 * 3600_000} onRetry={() => {}} />)
    expect(screen.getByText(/stale, the hourly job or the agent has stopped/)).toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Payment problems' })).getByText('1 order stuck over 2 h')).toBeInTheDocument()
  })

  it('explains an empty state and offers a retry on error', () => {
    const { rerender } = render(<SalesView status="ready" error={null} documents={[]} now={now} onRetry={() => {}} />)
    expect(screen.getByText(/No summary has arrived yet/)).toBeInTheDocument()
    const retry = vi.fn()
    rerender(<SalesView status="error" error="network" documents={[]} now={now} onRetry={retry} />)
    fireEvent.click(screen.getByRole('button', { name: 'retry' }))
    expect(retry).toHaveBeenCalled()
  })
})
