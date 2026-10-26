import { useEffect, useState } from 'react'
import { api, ApiError } from '../api/client'
import type { AnalyticsQuery, QueryResultDTO } from '../domain/analytics'

export interface QueryData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  result: QueryResultDTO | null
}

const LOADING: QueryData = { status: 'loading', error: null, result: null }

function queryPath(siteId: string): string {
  return `/api/sites/${encodeURIComponent(siteId)}/query`
}

/**
 * Runs one query of the analytics API (docs/SPEC.md §4i) and reruns it when the query changes. The
 * previous answer stays on screen while the next one loads, so a click does not blank a panel.
 */
export function useAnalyticsQuery(siteId: string, query: AnalyticsQuery): QueryData {
  const key = JSON.stringify(query)
  const [data, setData] = useState<QueryData>(LOADING)
  useEffect(() => {
    const controller = new AbortController()
    setData((d) => (d.status === 'ready' ? d : LOADING))
    api<QueryResultDTO>(queryPath(siteId), { method: 'POST', body: JSON.parse(key) as AnalyticsQuery, signal: controller.signal }).then(
      (result) => setData({ status: 'ready', error: null, result }),
      (e: unknown) => {
        if (controller.signal.aborted) return
        setData({ status: 'error', error: e instanceof Error ? e.message : 'load failed', result: null })
      },
    )
    return () => controller.abort()
  }, [siteId, key])
  return data
}

const REALTIME_MS = 30_000

/** Visitors with a pageview in the last five minutes, polled every 30 s; null until known. */
export function useRealtime(siteId: string): number | null {
  const [visitors, setVisitors] = useState<number | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    setVisitors(null)
    const load = () =>
      api<{ visitors: number }>(`/api/sites/${encodeURIComponent(siteId)}/realtime`, { signal: controller.signal }).then(
        (r) => setVisitors(r.visitors),
        () => undefined,
      )
    void load()
    // A hidden tab does not poll (Change 38); the next tick after it is shown refreshes the count.
    const timer = setInterval(() => {
      if (!document.hidden) void load()
    }, REALTIME_MS)
    return () => {
      clearInterval(timer)
      controller.abort()
    }
  }, [siteId])
  return visitors
}

/** Downloads a query as CSV: the endpoint is a POST, so the file goes through a blob link. */
export async function downloadCSV(siteId: string, query: AnalyticsQuery, filename: string): Promise<void> {
  const res = await fetch(queryPath(siteId), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...query, format: 'csv' }),
  })
  if (!res.ok) throw new ApiError('http', res.status, res.statusText || `HTTP ${res.status}`)
  const url = URL.createObjectURL(await res.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
