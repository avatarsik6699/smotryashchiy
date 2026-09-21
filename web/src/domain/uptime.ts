import type { UptimeResultDTO } from './types'

/** A certificate with this many days left or fewer is called out in text (alerting itself is deferred). */
export const TLS_WARN_DAYS = 14

export type UptimeState = 'up' | 'down' | 'stale' | 'new'

export const UPTIME_STATE_LABEL: Record<UptimeState, string> = { up: 'UP', down: 'DOWN', stale: 'STALE', new: 'NEW' }

const MIN_STALE_MS = 90_000

/** Derived like host state, never stored: a result that is too old says nothing about the target now. */
export function uptimeState(last: { ts: string; ok: boolean } | null, intervalSeconds: number, nowMs: number): UptimeState {
  if (last === null) return 'new'
  const staleAfter = Math.max(3 * intervalSeconds * 1000, MIN_STALE_MS)
  if (nowMs - Date.parse(last.ts) > staleAfter) return 'stale'
  return last.ok ? 'up' : 'down'
}

/** Whole days until the certificate expires (negative once expired), or null when unknown. */
export function tlsDaysLeft(last: Pick<UptimeResultDTO, 'cert_expires_at'> | null, nowMs: number): number | null {
  if (!last || last.cert_expires_at === null) return null
  return Math.floor((Date.parse(last.cert_expires_at) - nowMs) / 86_400_000)
}

/** "TLS 9d", "TLS expired" or null when there is no certificate information. */
export function tlsLabel(days: number | null): string | null {
  if (days === null) return null
  return days < 0 ? 'TLS expired' : `TLS ${days}d`
}
