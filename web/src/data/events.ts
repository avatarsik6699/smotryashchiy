import type { EventDTO } from '../domain/types'

/** The label that identifies an event's origin, in priority order (docs/SPEC.md §5, Change 12). */
export function eventSource(labels: Record<string, string>): string | null {
  return labels.unit ?? labels.container ?? labels.jail ?? null
}

/** Distinct sources present in events, sorted — the option list for the EVENTS filter. */
export function eventSources(events: EventDTO[]): string[] {
  const set = new Set<string>()
  for (const e of events) {
    const s = eventSource(e.labels)
    if (s !== null) set.add(s)
  }
  return [...set].sort()
}
