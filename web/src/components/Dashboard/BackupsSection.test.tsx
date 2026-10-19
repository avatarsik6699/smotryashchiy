import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { hostBackup } from '../../domain/backup'
import { BACKUP_NOW, sampleBackupDocument, sampleBackupSummary } from '../../domain/backup.fixture'
import { BackupsSection } from './BackupsSection'

const healthy = hostBackup(sampleBackupDocument(), BACKUP_NOW)
const failing = hostBackup(
  sampleBackupDocument(
    sampleBackupSummary({
      jobs: [
        { name: 'local', label: 'local backup', status: 'success', completed_at: '2026-10-19T02:29:00Z', max_age_hours: 36 },
        { name: 'offsite', label: 'off-site copy', status: 'failed', completed_at: '2026-10-17T02:31:00Z', max_age_hours: 36 },
      ],
      failed_units: ['infraege-backup.service'],
    }),
    'monitoring',
  ),
  BACKUP_NOW,
)

describe('BackupsSection', () => {
  it('shows one assessed row per host, worst finding first', () => {
    render(<BackupsSection status="ready" error={null} hosts={[healthy, failing]} now={BACKUP_NOW} onRetry={vi.fn()} />)
    const [ok, bad] = screen.getAllByRole('button', { name: /infraege-prod|monitoring/ })
    expect(ok).toHaveTextContent('OK')
    expect(ok).toHaveTextContent('4 jobs ok')
    expect(ok).toHaveTextContent('local 12 · offsite 12')
    expect(bad).toHaveTextContent('PROBLEM')
    expect(bad).toHaveTextContent('off-site copy: failed 2d ago')
    expect(bad).toHaveTextContent('+1 more')
  })

  it('expands a row to its jobs, repositories, holds and failed units', async () => {
    render(<BackupsSection status="ready" error={null} hosts={[failing]} now={BACKUP_NOW} onRetry={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /monitoring/ }))
    const jobs = screen.getByRole('table', { name: 'Backup jobs on monitoring' })
    expect(within(jobs).getByRole('row', { name: /off-site copy/ })).toHaveTextContent('problem')
    expect(screen.getByText('infraege-backup.service')).toBeInTheDocument()
    expect(screen.getByText(/94c5a3e7/)).toBeInTheDocument()
  })

  it('explains how to start reporting when no host sends a summary', () => {
    render(<BackupsSection status="ready" error={null} hosts={[]} now={BACKUP_NOW} onRetry={vi.fn()} />)
    expect(screen.getByText('--document backup=PATH')).toBeInTheDocument()
  })

  it('offers a retry when the summaries cannot be loaded', async () => {
    const onRetry = vi.fn()
    render(<BackupsSection status="error" error="network down" hosts={[]} now={BACKUP_NOW} onRetry={onRetry} />)
    await userEvent.click(screen.getByRole('button', { name: 'retry' }))
    expect(onRetry).toHaveBeenCalled()
  })

  it('has no detectable accessibility violations', async () => {
    const { container } = render(<BackupsSection status="ready" error={null} hosts={[healthy, failing]} now={BACKUP_NOW} onRetry={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /monitoring/ }))
    expect(await axe(container)).toHaveNoViolations()
  })
})
