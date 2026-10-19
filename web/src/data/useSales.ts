import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { SALES_SCHEMA, type BusinessDocument } from '../domain/sales'

export interface SalesData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  /** Business summaries the server holds, one per reporting host. */
  documents: BusinessDocument[]
  reload: () => void
}

/** The site writes its summary hourly; a five-minute refresh keeps an open tab current. */
const REFRESH_MS = 5 * 60_000

/** Fetches the latest business summaries (Change 27) and refreshes them while the view is open. */
export function useSales(): SalesData {
  const [state, setState] = useState<Omit<SalesData, 'reload'>>({ status: 'loading', error: null, documents: [] })
  const [key, setKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    api<{ documents: BusinessDocument[] }>('/api/documents?kind=business').then(
      (res) => {
        if (cancelled) return
        // A summary of another schema is shown as unknown, never misread as this one.
        setState({ status: 'ready', error: null, documents: res.documents.filter((d) => d.body?.schema === SALES_SCHEMA) })
      },
      (e: unknown) => {
        if (!cancelled)
          setState((s) => ({ status: s.status === 'ready' ? 'ready' : 'error', error: e instanceof Error ? e.message : 'load failed', documents: s.documents }))
      },
    )
    const timer = window.setTimeout(() => setKey((k) => k + 1), REFRESH_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [key])

  const reload = useCallback(() => setKey((k) => k + 1), [])
  return { ...state, reload }
}
