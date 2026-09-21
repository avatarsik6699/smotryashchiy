// Host state derives from freshness only (docs/SPEC.md §5): it says whether data is arriving, never
// whether a value is "good". Thresholds are the SPEC's: 45 s and 5 min.

export type HostState = 'ok' | 'stale' | 'offline' | 'new'

export const OK_WINDOW_MS = 45_000
export const STALE_WINDOW_MS = 5 * 60_000

/** Text shown for each state; state is never conveyed by color alone. */
export const HOST_STATE_LABEL: Record<HostState, string> = {
  ok: 'OK',
  stale: 'STALE',
  offline: 'OFFLINE',
  new: 'NEW',
}

export function hostState(lastSeenMs: number | null, nowMs: number): HostState {
  if (lastSeenMs === null) return 'new'
  const age = nowMs - lastSeenMs
  if (age <= OK_WINDOW_MS) return 'ok'
  if (age <= STALE_WINDOW_MS) return 'stale'
  return 'offline'
}

/** A current value is trusted only while its sample is recent; otherwise it is unknown (null). */
export const VALUE_MAX_AGE_MS = 5 * 60_000

export function isFresh(sampleMs: number, nowMs: number, maxAgeMs = VALUE_MAX_AGE_MS): boolean {
  return nowMs - sampleMs <= maxAgeMs
}
