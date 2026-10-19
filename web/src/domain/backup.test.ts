import { describe, expect, it } from 'vitest'
import { assessBackupDisk, assessHold, assessJob, hostBackup, parseBackupSummary, primaryJob } from './backup'
import { BACKUP_NOW, sampleBackupDocument, sampleBackupSummary } from './backup.fixture'

const job = sampleBackupSummary().jobs[0]!

describe('parseBackupSummary', () => {
  it('reads a well-formed summary', () => {
    const s = parseBackupSummary(sampleBackupSummary())
    expect(s?.jobs).toHaveLength(4)
    expect(s?.jobs[3]?.source).toBe('offsite')
  })

  it('refuses another schema or a malformed body instead of half-reading it', () => {
    expect(parseBackupSummary({ ...sampleBackupSummary(), schema: 'other/1' })).toBeNull()
    expect(parseBackupSummary({ ...sampleBackupSummary(), jobs: [{ name: 'x' }] })).toBeNull()
    expect(parseBackupSummary({ ...sampleBackupSummary(), disk: { path: '/', free_bytes: 1, total_bytes: 0 } })).toBeNull()
    expect(parseBackupSummary('nope')).toBeNull()
  })

  it('accepts a host that does not report its disk', () => {
    expect(parseBackupSummary(sampleBackupSummary({ disk: null }))?.disk).toBeNull()
  })
})

describe('assessJob', () => {
  it('is normal within its own limit', () => {
    expect(assessJob(job, BACKUP_NOW)).toEqual({ level: 'normal', reason: 'local backup: ok 9h ago' })
  })

  it('is a problem when older than its limit, failed or never completed', () => {
    expect(assessJob({ ...job, completed_at: '2026-10-17T12:00:00Z' }, BACKUP_NOW)).toEqual({
      level: 'problem',
      reason: 'local backup: last success 2d ago (problem > 36h)',
    })
    expect(assessJob({ ...job, status: 'failed' }, BACKUP_NOW).level).toBe('problem')
    expect(assessJob({ ...job, completed_at: null }, BACKUP_NOW).reason).toBe('local backup: never completed')
  })

  it('is watch when the job is not set up yet', () => {
    expect(assessJob({ ...job, status: 'not-configured', completed_at: null }, BACKUP_NOW)).toEqual({
      level: 'watch',
      reason: 'local backup: not configured',
    })
  })
})

describe('holds, disk and units', () => {
  it('asks to review a hold kept past its review date', () => {
    const hold = { name: '94c5a3e7', created_at: '2026-09-15T00:00:00Z', review_after_days: 30 }
    expect(assessHold(hold, BACKUP_NOW).level).toBe('watch')
    expect(assessHold({ ...hold, created_at: '2026-10-10T00:00:00Z' }, BACKUP_NOW).level).toBe('normal')
  })

  it('judges free space on the backup disk', () => {
    const disk = { path: '/b', free_bytes: 10, total_bytes: 100 }
    expect(assessBackupDisk(disk).level).toBe('watch')
    expect(assessBackupDisk({ ...disk, free_bytes: 4 }).level).toBe('problem')
    expect(assessBackupDisk({ ...disk, free_bytes: 50 }).level).toBe('normal')
    expect(assessBackupDisk(null).level).toBe('unknown')
  })
})

describe('hostBackup', () => {
  it('is normal for a healthy summary', () => {
    const h = hostBackup(sampleBackupDocument(), BACKUP_NOW)
    expect(h.overall).toBe('normal')
    expect(h.issues).toEqual([])
  })

  it('lists problems before watch items', () => {
    const body = sampleBackupSummary({
      jobs: [
        { ...job, name: 'offsite', label: 'off-site copy', status: 'not-configured', completed_at: null },
        { ...job, status: 'failed' },
      ],
      failed_units: ['infraege-backup.service'],
    })
    const h = hostBackup(sampleBackupDocument(body), BACKUP_NOW)
    expect(h.overall).toBe('problem')
    expect(h.issues.map((i) => i.level)).toEqual(['problem', 'problem', 'watch'])
  })

  it('makes a summary that stopped arriving a problem', () => {
    const h = hostBackup(sampleBackupDocument(sampleBackupSummary({ generated_at: '2026-10-19T08:00:00Z' })), BACKUP_NOW)
    expect(h.overall).toBe('problem')
    expect(h.summaryAge.reason).toContain('backup job or agent stopped')
  })

  it('shows another schema as unknown', () => {
    const h = hostBackup(sampleBackupDocument({ schema: 'kodumka.business/1' }), BACKUP_NOW)
    expect(h.summary).toBeNull()
    expect(h.overall).toBe('unknown')
  })

  it('takes the first job as the primary backup, so a passing check cannot hide a failing backup', () => {
    const body = sampleBackupSummary({ jobs: [{ ...job, status: 'failed' }, ...sampleBackupSummary().jobs.slice(1)] })
    const primary = primaryJob(hostBackup(sampleBackupDocument(body), BACKUP_NOW))
    expect(primary?.job.name).toBe('local')
    expect(primary?.assessment.level).toBe('problem')
  })
})
