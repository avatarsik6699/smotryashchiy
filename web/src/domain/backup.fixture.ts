import type { BackupDocument, BackupSummary } from './backup'

/** "Now" for the samples: 2026-10-19 12:00 UTC. */
export const BACKUP_NOW = Date.parse('2026-10-19T12:00:00Z')

/** A healthy site summary: local and off-site copies, a check, a restore drill and one hold. */
export function sampleBackupSummary(overrides: Partial<BackupSummary> = {}): BackupSummary {
  return {
    schema: 'smotryashchiy.backup/1',
    generated_at: '2026-10-19T11:30:00Z',
    jobs: [
      { name: 'local', label: 'local backup', status: 'success', completed_at: '2026-10-19T02:29:00Z', max_age_hours: 36, duration_seconds: 41 },
      { name: 'offsite', label: 'off-site copy', status: 'success', completed_at: '2026-10-19T02:31:00Z', max_age_hours: 36, duration_seconds: 12 },
      { name: 'offsite.check', label: 'off-site check', status: 'success', completed_at: '2026-10-14T04:00:00Z', max_age_hours: 192 },
      { name: 'restore', label: 'restore drill', status: 'success', completed_at: '2026-10-02T10:09:00Z', max_age_hours: 840, duration_seconds: 69, source: 'offsite' },
    ],
    repositories: [
      { name: 'local', snapshots: 12, size_bytes: 4_300_000 },
      { name: 'offsite', snapshots: 12, size_bytes: 4_100_000 },
    ],
    holds: [{ name: '94c5a3e7', created_at: '2026-10-10T20:55:00Z', review_after_days: 30 }],
    disk: { path: '/var/backups/infraege', free_bytes: 29_000_000_000, total_bytes: 40_000_000_000 },
    failed_units: [],
    ...overrides,
  }
}

export function sampleBackupDocument(body: unknown = sampleBackupSummary(), host = 'infraege-prod'): BackupDocument {
  return { host_id: `id-${host}`, host_name: host, received_at: '2026-10-19T11:31:00Z', body }
}
