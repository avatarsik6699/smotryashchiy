import { HOST_STATE_LABEL, type HostState } from '../../domain/freshness'
import { formatPercent, UNKNOWN } from '../../domain/format'
import type { HostView } from '../../data/model'
import styles from './StatusStrip.module.css'
import shared from './Dashboard.module.css'

export interface HostEntry {
  view: HostView
  state: HostState
}

function average(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v !== null)
  return known.length === 0 ? null : known.reduce((a, b) => a + b, 0) / known.length
}

export function summarizeHosts(entries: HostEntry[]) {
  const count = (state: HostState) => entries.filter((e) => e.state === state).length
  return {
    hosts: entries.length,
    ok: count('ok'),
    stale: count('stale'),
    offline: count('offline'),
    new: count('new'),
    cpu: average(entries.map((e) => e.view.cpu.value)),
    memory: average(entries.map((e) => e.view.memory.value)),
  }
}

/** One aligned row of fleet counters; unknown averages stay "—" rather than becoming 0. */
export interface ProbeSummary {
  up: number
  down: number
}

/** Probes with no verdict (NEW, STALE) are counted in neither number: unknown stays unknown. */
export function StatusStrip({ entries, probes }: { entries: HostEntry[]; probes: ProbeSummary | null }) {
  const s = summarizeHosts(entries)
  const items: [string, string][] = [
    ['hosts', String(s.hosts)],
    [HOST_STATE_LABEL.ok.toLowerCase(), String(s.ok)],
    [HOST_STATE_LABEL.stale.toLowerCase(), String(s.stale)],
    [HOST_STATE_LABEL.offline.toLowerCase(), String(s.offline)],
    [HOST_STATE_LABEL.new.toLowerCase(), String(s.new)],
    ['avg cpu', s.cpu === null ? UNKNOWN : formatPercent(s.cpu)],
    ['avg mem', s.memory === null ? UNKNOWN : formatPercent(s.memory)],
    ...(probes === null ? [] : ([['probes up', String(probes.up)], ['probes down', String(probes.down)]] as [string, string][])),
  ]
  return (
    <section className={shared.section} aria-labelledby="status-title">
      <h2 id="status-title" className={shared.sectionTitle}>
        Status
      </h2>
      <dl className={styles.strip}>
        {items.map(([label, value]) => (
          <div key={label} className={styles.item}>
            <dt className={styles.label}>{label}</dt>
            <dd className={styles.value}>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
