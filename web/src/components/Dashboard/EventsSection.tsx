import { Select } from '@base-ui/react/select'
import { useMemo, useState } from 'react'
import { eventSource, eventSources } from '../../data/events'
import { formatClock } from '../../domain/format'
import type { EventDTO } from '../../domain/types'
import shared from './Dashboard.module.css'
import styles from './EventsSection.module.css'

interface EventsSectionProps {
  events: EventDTO[]
  hostNames: Map<string, string>
}

/**
 * The system log: newest first, level as text, host by name. One filter — by source label
 * (`unit`/`container`/`jail`) — is the sole exception to the dashboard's no-filters rule
 * (docs/SPEC.md §5, Change 12): the journald/Docker-log/fail2ban collectors made an unfiltered
 * stream too broad to scan.
 */
export function EventsSection({ events, hostNames }: EventsSectionProps) {
  const [source, setSource] = useState<string | null>(null)
  const sources = useMemo(() => eventSources(events), [events])
  const filtered = useMemo(() => (source === null ? events : events.filter((e) => eventSource(e.labels) === source)), [events, source])
  const items = useMemo(() => [{ value: null, label: 'all sources' }, ...sources.map((s) => ({ value: s, label: s }))], [sources])

  return (
    <section className={shared.section} aria-labelledby="events-title">
      <div className={styles.header}>
        <h2 id="events-title" className={shared.sectionTitle}>
          Events
        </h2>
        {sources.length > 0 && (
          <Select.Root items={items} value={source} onValueChange={setSource}>
            <Select.Trigger className={styles.filterTrigger} aria-label="Filter events by source">
              <Select.Value />
            </Select.Trigger>
            <Select.Portal>
              <Select.Positioner className={styles.filterPositioner} sideOffset={4} align="end">
                <Select.Popup className={styles.filterPopup}>
                  {items.map((item) => (
                    <Select.Item key={item.value ?? ''} value={item.value} className={styles.filterItem}>
                      <Select.ItemText>{item.label}</Select.ItemText>
                    </Select.Item>
                  ))}
                </Select.Popup>
              </Select.Positioner>
            </Select.Portal>
          </Select.Root>
        )}
      </div>
      {filtered.length === 0 ? (
        <p className={shared.notice}>{events.length === 0 ? 'No events reported.' : 'No events from this source.'}</p>
      ) : (
        <ol className={styles.log}>
          {filtered.map((e) => {
            const src = eventSource(e.labels)
            return (
              <li key={`${e.host}|${e.ts}|${e.level}|${e.message}`} className={styles.entry}>
                <time className={styles.time} dateTime={e.ts}>
                  {formatClock(Date.parse(e.ts))}
                </time>
                <span className={styles.level} data-level={e.level}>
                  {e.level.toUpperCase()}
                </span>
                <span className={styles.host}>{hostNames.get(e.host) ?? e.host}</span>
                <span className={styles.message}>
                  {src && <span className={styles.source}>[{src}] </span>}
                  {e.message}
                </span>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
