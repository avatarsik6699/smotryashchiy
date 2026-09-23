import { Accordion } from '@base-ui/react/accordion'
import { formatAge, formatPercent, formatRate, formatUptime, UNKNOWN } from '../../domain/format'
import { HOST_STATE_LABEL } from '../../domain/freshness'
import { hostHealth } from '../../data/health'
import type { HostRecord } from '../../data/model'
import type { Assessment } from '../../domain/health'
import { LevelMark, toneClass } from '../Health/LevelMark'
import { Chart } from '../Chart/Chart'
import { HostDetails } from './HostDetails'
import type { HostEntry } from './StatusStrip'
import styles from './HostRow.module.css'

interface HostRowProps {
  entry: HostEntry
  record: HostRecord
  now: number
}

interface MetricProps {
  label: string
  value: string
  points: { t: number; v: number }[]
  kind: 'percent' | 'auto'
  format: (v: number) => string
  title?: string
  /** Absent for values that are never assessed (network). */
  assessment?: Assessment
}

/** One measurement: label, current value (text, "—" when unknown) and a decorative sparkline. */
function Metric({ label, value, points, kind, format, title, assessment }: MetricProps) {
  return (
    <span className={styles.metric} title={title}>
      <span className={styles.metricHead}>
        <span className={styles.metricLabel}>{label}</span>
        <span className={`${styles.metricValue} ${toneClass}`} data-level={assessment?.level}>
          {value}
          {assessment && <LevelMark assessment={assessment} />}
        </span>
      </span>
      <Chart series={[{ label, points }]} variant="spark" height={28} kind={kind} format={format} name={`${label}, last hour`} decorative />
    </span>
  )
}

export function HostRow({ entry, record, now }: HostRowProps) {
  const { view, state } = entry
  const value = (v: number | null, fmt: (n: number) => string) => (v === null ? UNKNOWN : fmt(v))
  const age = view.lastSeenMs === null ? 'never seen' : formatAge(now - view.lastSeenMs)
  const meta = view.uptimeSeconds === null ? null : `up ${formatUptime(view.uptimeSeconds)}`
  const diskLabel = view.disk.mount === null ? 'DISK' : `DISK ${view.disk.mount}`
  const health = hostHealth(record, now)
  return (
    <Accordion.Item value={view.id} className={styles.item} data-state={state}>
      <Accordion.Header className={styles.header}>
        <Accordion.Trigger className={styles.trigger}>
          <span className={styles.state}>
            <svg className={styles.chevron} width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" aria-hidden="true">
              <path d="M3 1.5 6.5 5 3 8.5" />
            </svg>
            <span className={styles.pulse} aria-hidden="true" />
            {HOST_STATE_LABEL[state]}
          </span>
          <span className={styles.identity}>
            <span className={styles.name}>{view.name}</span>
            {meta && <span className={styles.meta}>{meta}</span>}
          </span>
          <Metric label="CPU" value={value(view.cpu.value, formatPercent)} points={view.cpu.points} kind="percent" format={formatPercent} assessment={health.cpu} />
          <Metric label="MEM" value={value(view.memory.value, formatPercent)} points={view.memory.points} kind="percent" format={formatPercent} assessment={health.memory} />
          <Metric label={diskLabel} value={value(view.disk.value, formatPercent)} points={view.disk.points} kind="percent" format={formatPercent} title={view.disk.mount ?? undefined} assessment={health.disk} />
          <Metric label="NET" value={value(view.network.rate, formatRate)} points={view.network.points} kind="auto" format={formatRate} />
          <span className={styles.age}>{age}</span>
        </Accordion.Trigger>
      </Accordion.Header>
      <Accordion.Panel className={styles.panel}>
        <HostDetails record={record} now={now} />
      </Accordion.Panel>
    </Accordion.Item>
  )
}
