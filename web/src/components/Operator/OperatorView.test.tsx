import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { sampleIncident, sampleOperatorDocument, sampleReport } from '../../domain/operator.fixture'
import { OperatorView } from './OperatorView'

const now = new Date('2026-10-24T12:10:00Z').getTime()

function view(overrides: Partial<Parameters<typeof OperatorView>[0]> = {}) {
  return render(
    <OperatorView
      status="ready"
      error={null}
      documents={[sampleOperatorDocument()]}
      commands={[]}
      incidents={[]}
      audit={[]}
      now={now}
      onReload={() => {}}
      {...overrides}
    />,
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('OperatorView (Change 30)', () => {
  it('shows signals with their sessions, measures and refunds', async () => {
    const { container } = view()
    expect(screen.getByRole('heading', { name: 'Operator' })).toBeInTheDocument()
    expect(screen.getByText(/kodumka-prod · report 10m ago/)).toBeInTheDocument()
    const signals = screen.getByRole('region', { name: 'Signals' })
    expect(within(signals).getByText('shared@example.com')).toBeInTheDocument()
    expect(within(signals).getByText('several cities in one day, devices pushed out')).toBeInTheDocument()
    expect(within(signals).getByText('Москва · 198.51.100.7 · Chrome · Windows')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Restrictions' })).getByText('доступ передан другим людям')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Refunds' })).getByText('deleted account')).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it('asks for the reason before warning and for the password before terminating', async () => {
    const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
      new Response(
        JSON.stringify({
          command: { id: 'c1', host_id: 'host-1', kind: JSON.parse(String(init?.body ?? '{}')).kind, args: {}, status: 'done', created_at: '', started_at: null, finished_at: null, result: { ok: true } },
        }),
        { status: 202, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    vi.stubGlobal('fetch', fetch)
    view()
    fireEvent.click(screen.getByRole('button', { name: 'Terminate paid access of shared@example.com' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/3 cities in one day, 4 sessions pushed out/)).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Admin password')).toBeRequired()
    const reason = within(dialog).getByLabelText('Reason the student reads (Russian)')
    expect(reason).toBeRequired()
    fireEvent.change(reason, { target: { value: 'повторная передача доступа' } })
    fireEvent.change(within(dialog).getByLabelText('Admin password'), { target: { value: 'secret-password-1234567890' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Terminate paid access' }))
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    const body = JSON.parse(String(fetch.mock.calls[0]![1]!.body))
    expect(body).toMatchObject({
      host_id: 'host-1',
      kind: 'terminate',
      args: { email: 'shared@example.com' },
      reason: 'повторная передача доступа',
      password: 'secret-password-1234567890',
    })
    expect(await within(dialog).findByText('done')).toBeInTheDocument()
  })

  it('counts down the RKN deadlines of an open leak', () => {
    view({ incidents: [sampleIncident()] })
    const incidents = screen.getByRole('region', { name: 'Incidents' })
    expect(within(incidents).getByText(/primary RKN notice \(24 h\): 11 h 50 min left/)).toBeInTheDocument()
    expect(within(incidents).getByRole('button', { name: 'primary RKN notice sent' })).toBeInTheDocument()
  })

  it('explains the empty state when no report has arrived', () => {
    view({ documents: [] })
    expect(screen.getByText(/No operator report has arrived yet/)).toBeInTheDocument()
    expect(screen.getByText('No command has been sent yet.')).toBeInTheDocument()
  })

  it('names a report in another schema as unknown instead of misreading it', () => {
    view({ documents: [sampleOperatorDocument(sampleReport({ schema: 'kodumka.operator/2' }))] })
    expect(screen.getByText(/is in an unknown format \(kodumka\.operator\/2\)/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Signals' })).not.toBeInTheDocument()
  })

  it('shows a report that stopped arriving as stale', () => {
    view({ documents: [sampleOperatorDocument(sampleReport({ generated_at: '2026-10-24T10:00:00+00:00' }))] })
    expect(screen.getByText(/stale, the ten-minute job or the agent has stopped/)).toBeInTheDocument()
  })
})
