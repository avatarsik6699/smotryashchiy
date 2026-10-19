import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import type { BackupDocument } from '../domain/backup'
import { REFRESH_INTERVAL_MS } from './store'

export interface BackupsData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  /** Every `backup` document the server holds, one per reporting host, in any schema. */
  documents: BackupDocument[]
  reload: () => void
}

/**
 * Backup summaries (Change 29). They feed the BACKUPS ledger and the summary line, so they load
 * with the dashboard and refresh on its full-refresh cadence; nothing streams them.
 */
export function useBackups(): BackupsData {
  const [state, setState] = useState<Omit<BackupsData, 'reload'>>({ status: 'loading', error: null, documents: [] })
  const [key, setKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    api<{ documents: BackupDocument[] }>('/api/documents?kind=backup').then(
      (res) => {
        if (!cancelled) setState({ status: 'ready', error: null, documents: res.documents })
      },
      (e: unknown) => {
        if (!cancelled)
          setState((s) => ({ status: s.status === 'ready' ? 'ready' : 'error', error: e instanceof Error ? e.message : 'load failed', documents: s.documents }))
      },
    )
    const timer = window.setTimeout(() => setKey((k) => k + 1), REFRESH_INTERVAL_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [key])

  const reload = useCallback(() => setKey((k) => k + 1), [])
  return { ...state, reload }
}
