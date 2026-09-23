import { HOST_STATE_LABEL, type HostState } from '../../domain/freshness'
import { formatPercent, UNKNOWN } from '../../domain/format'
import type { HostView } from '../../data/model'
import type { Level } from '../../domain/health'
import { HelpButton } from '../Help/HelpButton'
import { LevelMark, toneClass } from '../Health/LevelMark'
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
  // A count that is above zero for a bad state is itself the finding (Change 22 assessment).
  const flag = (n: number, level: Level): Level | undefined => (n > 0 ? level : undefined)
  const items: [string, string, Level?][] = [
    ['hosts', String(s.hosts)],
    [HOST_STATE_LABEL.ok.toLowerCase(), String(s.ok)],
    [HOST_STATE_LABEL.stale.toLowerCase(), String(s.stale), flag(s.stale, 'watch')],
    [HOST_STATE_LABEL.offline.toLowerCase(), String(s.offline), flag(s.offline, 'problem')],
    [HOST_STATE_LABEL.new.toLowerCase(), String(s.new)],
    ['avg cpu', s.cpu === null ? UNKNOWN : formatPercent(s.cpu)],
    ['avg mem', s.memory === null ? UNKNOWN : formatPercent(s.memory)],
    ...(probes === null ? [] : ([['probes up', String(probes.up)], ['probes down', String(probes.down), flag(probes.down, 'problem')]] as [string, string, Level?][])),
  ]
  return (
    <section className={shared.section} aria-labelledby="status-title">
      <div className={shared.titleRow}>
        <h2 id="status-title" className={shared.sectionTitle}>
          Status
        </h2>
        <HelpButton lesson="status" topic="the status strip" />
      </div>
      <dl className={styles.strip}>
        {items.map(([label, value, level]) => (
          <div key={label} className={styles.item}>
            <dt className={styles.label}>{label}</dt>
            <dd className={`${styles.value} ${toneClass}`} data-level={level}>
              {value}
              {level && <LevelMark assessment={{ level, reason: `${value} ${label}` }} />}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
