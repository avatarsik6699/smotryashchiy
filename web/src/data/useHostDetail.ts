import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { EventDTO, MetricDTO } from '../domain/types'
import { DETAIL_METRICS } from './detail'
import { WINDOW_MS } from './model'

export interface HostDetailData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  raw: MetricDTO[]
  events: EventDTO[]
}

const LOADING: HostDetailData = { status: 'loading', error: null, raw: [], events: [] }

/** Fetches the raw (10 s) last hour and recent events of one host when its row is expanded. */
export function useHostDetail(hostId: string, reloadKey = 0): HostDetailData {
  const [data, setData] = useState<HostDetailData>(LOADING)
  useEffect(() => {
    const controller = new AbortController()
    const from = encodeURIComponent(new Date(Date.now() - WINDOW_MS).toISOString())
    const host = encodeURIComponent(hostId)
    setData((d) => (d.status === 'ready' ? d : LOADING))
    Promise.all([
      ...DETAIL_METRICS.map((name) => api<{ metrics: MetricDTO[] }>(`/api/metrics?host=${host}&name=${encodeURIComponent(name)}&from=${from}&limit=5000`, { signal: controller.signal })),
      api<{ events: EventDTO[] }>(`/api/events?host=${host}&limit=20`, { signal: controller.signal }),
    ]).then(
      (results) => {
        const events = (results[results.length - 1] as { events: EventDTO[] }).events
        const raw = (results.slice(0, -1) as { metrics: MetricDTO[] }[]).flatMap((r) => r.metrics)
        setData({ status: 'ready', error: null, raw, events })
      },
      (e: unknown) => {
        if (controller.signal.aborted) return
        setData({ status: 'error', error: e instanceof Error ? e.message : 'load failed', raw: [], events: [] })
      },
    )
    return () => controller.abort()
  }, [hostId, reloadKey])
  return data
}
