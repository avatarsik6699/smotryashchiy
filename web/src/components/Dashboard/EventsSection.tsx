import { formatClock } from '../../domain/format'
import type { EventDTO } from '../../domain/types'
import shared from './Dashboard.module.css'
import styles from './EventsSection.module.css'

interface EventsSectionProps {
  events: EventDTO[]
  hostNames: Map<string, string>
}

/** The system log: newest first, level as text, host by name. */
export function EventsSection({ events, hostNames }: EventsSectionProps) {
  return (
    <section className={shared.section} aria-labelledby="events-title">
      <h2 id="events-title" className={shared.sectionTitle}>
        Events
      </h2>
      {events.length === 0 ? (
        <p className={shared.notice}>No events reported.</p>
      ) : (
        <ol className={styles.log}>
          {events.map((e) => (
            <li key={`${e.host}|${e.ts}|${e.level}|${e.message}`} className={styles.entry}>
              <time className={styles.time} dateTime={e.ts}>
                {formatClock(Date.parse(e.ts))}
              </time>
              <span className={styles.level} data-level={e.level}>
                {e.level.toUpperCase()}
              </span>
              <span className={styles.host}>{hostNames.get(e.host) ?? e.host}</span>
              <span className={styles.message}>{e.message}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
