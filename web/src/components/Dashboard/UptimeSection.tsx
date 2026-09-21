import { Button } from '@base-ui/react/button'
import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { UptimeRecord } from '../../data/model'
import { useStore } from '../../data/DashboardContext'
import { formatAge, formatLatency, UNKNOWN } from '../../domain/format'
import { tlsDaysLeft, tlsLabel, TLS_WARN_DAYS, uptimeState, UPTIME_STATE_LABEL } from '../../domain/uptime'
import { Chart } from '../Chart/Chart'
import shared from './Dashboard.module.css'
import styles from './UptimeSection.module.css'

interface UptimeSectionProps {
  targets: UptimeRecord[]
  now: number
  onAdd: () => void
}

export function UptimeSection({ targets, now, onAdd }: UptimeSectionProps) {
  return (
    <section className={shared.section} aria-labelledby="uptime-title">
      <div className={styles.head}>
        <h2 id="uptime-title" className={shared.sectionTitle}>
          Uptime
        </h2>
        <Button className={shared.button} onClick={onAdd}>
          + add target
        </Button>
      </div>
      {targets.length === 0 ? (
        <p className={shared.notice}>No targets yet. Add one to probe an HTTP endpoint, a TCP port or a TLS certificate from the server.</p>
      ) : (
        <ul className={styles.ledger}>
          {targets.map((t) => (
            <UptimeRow key={t.target.id} record={t} now={now} />
          ))}
        </ul>
      )}
    </section>
  )
}

type RemoveState = { phase: 'idle' } | { phase: 'confirm' } | { phase: 'removing' } | { phase: 'failed'; message: string }

function UptimeRow({ record, now }: { record: UptimeRecord; now: number }) {
  const store = useStore()
  const [remove, setRemove] = useState<RemoveState>({ phase: 'idle' })
  const { target, last } = record
  const state = uptimeState(last, target.interval_seconds, now)
  const days = tlsDaysLeft(last, now)
  const tls = tlsLabel(days)
  const points = record.latency.filter((p): p is { t: number; v: number } => p.v !== null)
  const gaps = record.latency.filter((p) => p.v === null).map((p) => p.t)
  const latency = last?.latency_ms ?? null

  async function confirmRemove() {
    setRemove({ phase: 'removing' })
    try {
      await api(`/api/uptime/${encodeURIComponent(target.id)}`, { method: 'DELETE' })
      await store.load()
    } catch (e) {
      setRemove({ phase: 'failed', message: e instanceof ApiError ? e.message : 'could not remove the target' })
    }
  }

  return (
    <li className={styles.row} data-state={state}>
      <span className={styles.state}>
        <span className={styles.pulse} aria-hidden="true" />
        {UPTIME_STATE_LABEL[state]}
      </span>
      <span className={styles.identity}>
        <span className={styles.name}>{target.name}</span>
        <span className={styles.target}>
          {target.kind} {target.target}
        </span>
        {state === 'down' && last && <span className={styles.error}>{last.error || 'check failed'}</span>}
      </span>
      <span className={styles.latency}>
        <span className={styles.latencyHead}>
          <span className={styles.label}>LATENCY</span>
          <span className={styles.value}>{latency === null ? UNKNOWN : formatLatency(latency)}</span>
        </span>
        <Chart series={[{ label: 'latency', points, gaps }]} variant="spark" height={28} kind="auto" format={formatLatency} name={`${target.name} latency, last hour`} decorative />
      </span>
      <span className={styles.tls} data-warn={days !== null && days <= TLS_WARN_DAYS ? 'true' : undefined}>
        {tls ?? ''}
      </span>
      <span className={styles.age}>{last ? formatAge(now - Date.parse(last.ts)) : 'no result yet'}</span>
      <span className={styles.actions}>
        {remove.phase === 'idle' && (
          <Button className={shared.button} onClick={() => setRemove({ phase: 'confirm' })} aria-label={`Remove ${target.name}`}>
            remove
          </Button>
        )}
        {(remove.phase === 'confirm' || remove.phase === 'removing' || remove.phase === 'failed') && (
          <span className={styles.confirm} role="group" aria-label={`Confirm removing ${target.name}`}>
            <Button className={shared.buttonPrimary} disabled={remove.phase === 'removing'} focusableWhenDisabled onClick={() => void confirmRemove()}>
              {remove.phase === 'removing' ? 'removing…' : 'remove target and its history'}
            </Button>
            <Button className={shared.button} onClick={() => setRemove({ phase: 'idle' })}>
              cancel
            </Button>
          </span>
        )}
        {remove.phase === 'failed' && (
          <span role="alert" className={styles.error}>
            {remove.message}
          </span>
        )}
      </span>
    </li>
  )
}
