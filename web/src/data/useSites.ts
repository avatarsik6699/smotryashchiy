import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import type { SiteDTO } from '../domain/types'

export interface SitesData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  sites: SiteDTO[]
  reload: () => void
}

/** Fetches the site list; reload() re-fetches after a create/delete (no live stream for this domain, §4i). */
export function useSites(): SitesData {
  const [state, setState] = useState<{
    status: SitesData['status']
    error: string | null
    sites: SiteDTO[]
  }>({ status: 'loading', error: null, sites: [] })
  const [key, setKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    setState((s) => (s.status === 'ready' ? s : { status: 'loading', error: null, sites: [] }))
    api<{ sites: SiteDTO[] }>('/api/sites').then(
      (res) => {
        if (!cancelled) setState({ status: 'ready', error: null, sites: res.sites })
      },
      (e: unknown) => {
        if (!cancelled)
          setState({
            status: 'error',
            error: e instanceof Error ? e.message : 'load failed',
            sites: [],
          })
      },
    )
    return () => {
      cancelled = true
    }
  }, [key])

  const reload = useCallback(() => setKey((k) => k + 1), [])
  return { ...state, reload }
}
