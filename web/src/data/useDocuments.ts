import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'

export interface DocumentsData<T> {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  /** The documents of one kind the server holds, one per reporting host. */
  documents: T[]
  reload: () => void
}

/**
 * Loads the latest documents of `kind` (`/api/documents`) and refreshes them every `refreshMs`
 * while the view is open. `keep` drops documents the view cannot read, e.g. another schema, so
 * they show as unknown instead of being misread. A failed refresh keeps the last good list.
 */
export function useDocuments<T>(kind: string, refreshMs: number, keep?: (doc: T) => boolean): DocumentsData<T> {
  const [state, setState] = useState<Omit<DocumentsData<T>, 'reload'>>({ status: 'loading', error: null, documents: [] })
  const [key, setKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    api<{ documents: T[] }>(`/api/documents?kind=${encodeURIComponent(kind)}`).then(
      (res) => {
        if (!cancelled) setState({ status: 'ready', error: null, documents: keep ? res.documents.filter(keep) : res.documents })
      },
      (e: unknown) => {
        if (!cancelled)
          setState((s) => ({ status: s.status === 'ready' ? 'ready' : 'error', error: e instanceof Error ? e.message : 'load failed', documents: s.documents }))
      },
    )
    // A hidden tab waits for the moment it is shown to refresh (Change 38).
    const refresh = () => setKey((k) => k + 1)
    let waiting = false
    const onVisible = () => {
      if (!document.hidden && waiting) refresh()
    }
    const timer = window.setTimeout(() => {
      if (document.hidden) {
        waiting = true
        document.addEventListener('visibilitychange', onVisible)
      } else refresh()
    }, refreshMs)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
    // `keep` is a module-level predicate in every caller; the refresh key drives reloads.
  }, [key, kind, refreshMs])

  const reload = useCallback(() => setKey((k) => k + 1), [])
  return { ...state, reload }
}
