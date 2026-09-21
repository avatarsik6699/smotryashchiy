import { Accordion } from '@base-ui/react/accordion'
import type { HostRecord } from '../../data/model'
import { HostRow } from './HostRow'
import type { HostEntry } from './StatusStrip'
import shared from './Dashboard.module.css'
import styles from './HostsSection.module.css'

interface HostsSectionProps {
  entries: HostEntry[]
  records: HostRecord[]
  now: number
  onAddHost: () => void
}

/** The ledger: one row per host, one open at a time, details expand in place. */
export function HostsSection({ entries, records, now, onAddHost }: HostsSectionProps) {
  return (
    <section className={shared.section} aria-labelledby="hosts-title">
      <h2 id="hosts-title" className={shared.sectionTitle}>
        Hosts
      </h2>
      {entries.length === 0 ? (
        <p className={shared.notice}>
          No hosts yet.{' '}
          <button type="button" className={styles.link} onClick={onAddHost}>
            Add a host
          </button>{' '}
          to get its enrollment command.
        </p>
      ) : (
        <Accordion.Root className={styles.ledger}>
          {entries.map((entry, i) => (
            <HostRow key={entry.view.id} entry={entry} record={records[i]!} now={now} />
          ))}
        </Accordion.Root>
      )}
    </section>
  )
}
