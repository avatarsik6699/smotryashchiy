import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { SiteStatsDTO, StatsRange } from '../domain/types'

export interface SiteStatsData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  stats: SiteStatsDTO | null
}

const LOADING: SiteStatsData = { status: 'loading', error: null, stats: null }

/** Fetches one site's pageview summary for the given range (docs/SPEC.md §4i). */
export function useSiteStats(siteId: string, range: StatsRange): SiteStatsData {
  const [data, setData] = useState<SiteStatsData>(LOADING)
  useEffect(() => {
    const controller = new AbortController()
    setData((d) => (d.status === 'ready' ? d : LOADING))
    api<SiteStatsDTO>(`/api/sites/${encodeURIComponent(siteId)}/stats?range=${encodeURIComponent(range)}`, { signal: controller.signal }).then(
      (stats) => setData({ status: 'ready', error: null, stats }),
      (e: unknown) => {
        if (controller.signal.aborted) return
        setData({
          status: 'error',
          error: e instanceof Error ? e.message : 'load failed',
          stats: null,
        })
      },
    )
    return () => controller.abort()
  }, [siteId, range])
  return data
}
