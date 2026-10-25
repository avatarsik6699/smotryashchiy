import type { BackupDocument } from '../domain/backup'
import { REFRESH_INTERVAL_MS } from './store'
import { useDocuments, type DocumentsData } from './useDocuments'

export type BackupsData = DocumentsData<BackupDocument>

/**
 * Backup summaries (Change 29), every `backup` document in any schema. They feed the BACKUPS
 * ledger and the summary line, so they load with the dashboard and refresh on its full-refresh
 * cadence; nothing streams them.
 */
export function useBackups(): BackupsData {
  return useDocuments<BackupDocument>('backup', REFRESH_INTERVAL_MS)
}
