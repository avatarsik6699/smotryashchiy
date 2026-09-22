import { Meter } from '@base-ui/react/meter'
import { useMemo } from 'react'
import { buildDetailSeries, containerInfos, diskInfos, mergeHostEvents } from '../../data/detail'
import { useDashboard } from '../../data/DashboardContext'
import { eventSource } from '../../data/events'
import type { HostRecord } from '../../data/model'
import { useHostDetail } from '../../data/useHostDetail'
import { formatAge, formatBytes, formatClock, formatMeta, formatPercent, formatRate, UNKNOWN } from '../../domain/format'
import { Chart } from '../Chart/Chart'
import styles from './HostDetails.module.css'

const CHART_HEIGHT = 150
const load = (v: number) => v.toFixed(2)
const MAX_INTERFACES = 8

/** Everything about one host, shown in place under its row. The window is fixed at one hour. */
export function HostDetails({ record, now }: { record: HostRecord; now: number }) {
  const detail = useHostDetail(record.host.id)
  const { events: liveEvents } = useDashboard()
  const series = useMemo(() => buildDetailSeries(detail.raw, record, now), [detail.raw, record, now])
  const disks = useMemo(() => diskInfos(record, now), [record, now])
  const containers = useMemo(() => containerInfos(record, now), [record, now])
  // Busy interfaces first; container hosts can have dozens of idle veth devices.
  const interfaces = useMemo(() => [...series.interfaces].sort((a, b) => (b.rx ?? 0) + (b.tx ?? 0) - ((a.rx ?? 0) + (a.tx ?? 0)) || a.name.localeCompare(b.name)), [series.interfaces])
  const events = useMemo(() => mergeHostEvents(detail.events, liveEvents, record.host.id), [detail.events, liveEvents, record.host.id])

  if (detail.status === 'loading') {
    return (
      <p className={styles.notice} role="status">
        loading {record.host.name}…
      </p>
    )
  }
  return (
    <div className={styles.details}>
      {detail.status === 'error' && (
        <p className={styles.notice} role="alert">
          Could not load the full history ({detail.error}); showing what the overview already has.
        </p>
      )}
      <div className={styles.charts}>
        <Chart series={[{ label: 'CPU', points: series.cpu }]} variant="full" height={CHART_HEIGHT} kind="percent" format={formatPercent} name="CPU usage, last hour" />
        <Chart
          series={[
            { label: 'memory', points: series.memory },
            { label: 'swap', points: series.swap },
          ]}
          variant="full"
          height={CHART_HEIGHT}
          kind="percent"
          format={formatPercent}
          name="Memory and swap usage, last hour"
        />
        <Chart
          series={[
            { label: 'load 1m', points: series.load1 },
            { label: 'load 5m', points: series.load5 },
            { label: 'load 15m', points: series.load15 },
          ]}
          variant="full"
          height={CHART_HEIGHT}
          kind="auto"
          format={load}
          name="Load average, last hour"
        />
        <Chart
          series={[
            { label: 'rx', points: series.rx },
            { label: 'tx', points: series.tx },
          ]}
          variant="full"
          height={CHART_HEIGHT}
          kind="auto"
          format={formatRate}
          name="Network throughput, last hour"
        />
      </div>

      <div className={styles.tables}>
        <section className={styles.block} aria-label="Disks">
          <h3 className={styles.blockTitle}>Disks</h3>
          {disks.length === 0 ? (
            <p className={styles.muted}>no disk data</p>
          ) : (
            <ul className={styles.list}>
              {disks.map((d) => (
                <li key={d.mount} className={styles.disk}>
                  <span className={styles.diskName}>
                    {d.mount}
                    <span className={styles.muted}> {d.device}</span>
                  </span>
                  {d.usedPercent === null ? (
                    <span className={styles.muted}>{UNKNOWN}</span>
                  ) : (
                    <Meter.Root className={styles.meter} value={d.usedPercent} min={0} max={100} aria-label={`${d.mount} used`} getAriaValueText={(_f, v) => formatPercent(v)}>
                      <Meter.Track className={styles.track}>
                        <Meter.Indicator className={styles.indicator} />
                      </Meter.Track>
                    </Meter.Root>
                  )}
                  <span className={styles.diskText}>
                    {d.usedPercent === null ? UNKNOWN : formatPercent(d.usedPercent)}
                    <span className={styles.muted}> {d.usedBytes === null || d.totalBytes === null ? '' : `${formatBytes(d.usedBytes)} / ${formatBytes(d.totalBytes)}`}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        {containers.length > 0 && (
          <section className={styles.block} aria-label="Containers">
            <h3 className={styles.blockTitle}>Containers</h3>
            <ul className={styles.list}>
              {containers.map((c) => (
                <li key={c.name} className={styles.container}>
                  <span className={styles.containerName}>
                    {c.name}
                    <span className={styles.muted}> {c.image}</span>
                  </span>
                  <span className={styles.containerText}>{c.cpuPercent === null ? UNKNOWN : formatPercent(c.cpuPercent)}</span>
                  <span className={styles.containerText}>
                    {c.memUsedPercent === null ? UNKNOWN : formatPercent(c.memUsedPercent)}
                    <span className={styles.muted}> {c.memUsedBytes === null ? '' : formatBytes(c.memUsedBytes)}</span>
                  </span>
                </li>
              ))}
            </ul>
          </section>
        )}

        <section className={styles.block} aria-label="Network interfaces">
          <h3 className={styles.blockTitle}>Interfaces</h3>
          {interfaces.length === 0 ? (
            <p className={styles.muted}>no interface data</p>
          ) : (
            <ul className={styles.list}>
              {interfaces.slice(0, MAX_INTERFACES).map((i) => (
                <li key={i.name} className={styles.row}>
                  <span>{i.name || 'default'}</span>
                  <span className={styles.muted}>
                    rx {i.rx === null ? UNKNOWN : formatRate(i.rx)} · tx {i.tx === null ? UNKNOWN : formatRate(i.tx)}
                  </span>
                </li>
              ))}
              {interfaces.length > MAX_INTERFACES && <li className={styles.muted}>+ {interfaces.length - MAX_INTERFACES} more, quieter</li>}
            </ul>
          )}
        </section>

        <section className={styles.block} aria-label="Checks">
          <h3 className={styles.blockTitle}>Checks</h3>
          {record.checks.length === 0 ? (
            <p className={styles.muted}>no checks reported</p>
          ) : (
            <ul className={styles.list}>
              {record.checks.map((c) => {
                const meta = formatMeta(c.meta)
                return (
                  <li key={c.name} className={styles.row}>
                    <span>{c.name}</span>
                    <span className={styles.check} data-status={c.status}>
                      {c.status.toUpperCase()} <span className={styles.muted}>{formatAge(now - Date.parse(c.ts))}</span>
                      {meta && <span className={styles.muted}> · {meta}</span>}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </section>

        <section className={styles.block} aria-label="Host events">
          <h3 className={styles.blockTitle}>Events</h3>
          {events.length === 0 ? (
            <p className={styles.muted}>no events</p>
          ) : (
            <ul className={styles.list}>
              {events.map((e) => {
                const source = eventSource(e.labels)
                return (
                  <li key={`${e.ts}|${e.level}|${e.message}`} className={styles.event}>
                    <span className={styles.muted}>{formatClock(Date.parse(e.ts))}</span>
                    <span className={styles.level} data-level={e.level}>
                      {e.level.toUpperCase()}
                    </span>
                    <span className={styles.message}>
                      {source && <span className={styles.muted}>[{source}] </span>}
                      {e.message}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      </div>
    </div>
  )
}
