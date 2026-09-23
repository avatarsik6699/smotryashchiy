import { Meter } from '@base-ui/react/meter'
import { useMemo } from 'react'
import { buildDetailSeries, containerInfos, diskInfos, mergeHostEvents } from '../../data/detail'
import { useDashboard } from '../../data/DashboardContext'
import { eventSource } from '../../data/events'
import type { HostRecord } from '../../data/model'
import { useHostDetail } from '../../data/useHostDetail'
import { formatAge, formatBytes, formatClock, formatMeta, formatPercent, formatRate, UNKNOWN } from '../../domain/format'
import { hostHealth } from '../../data/health'
import { assessPercent, type Assessment } from '../../domain/health'
import type { LessonId } from '../../guide/lessons'
import { Chart } from '../Chart/Chart'
import { HelpButton } from '../Help/HelpButton'
import { LevelMark, toneClass } from '../Health/LevelMark'
import styles from './HostDetails.module.css'

const CHART_HEIGHT = 150
const load = (v: number) => v.toFixed(2)
const MAX_INTERFACES = 8

/** A block title with its "?" help; the heading keeps only its own text. */
function BlockTitle({ title, lesson, topic }: { title: string; lesson: LessonId; topic: string }) {
  return (
    <div className={styles.blockHead}>
      <h3 className={styles.blockTitle}>{title}</h3>
      <HelpButton lesson={lesson} topic={topic} />
    </div>
  )
}

/** One assessed value in a chart's heading line, e.g. "load 0.20 on 2 CPUs = 0.10 per CPU". */
function Now({ assessment }: { assessment: Assessment }) {
  return (
    <span className={`${styles.now} ${toneClass}`} data-level={assessment.level}>
      {assessment.reason}
      <LevelMark assessment={assessment} />
    </span>
  )
}

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
  const health = useMemo(() => hostHealth(record, now), [record, now])

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
        <div className={styles.chartBlock}>
          <BlockTitle title="CPU" lesson="cpu" topic="CPU" />
          <Now assessment={health.cpu} />
          <Chart series={[{ label: 'CPU', points: series.cpu }]} variant="full" height={CHART_HEIGHT} kind="percent" format={formatPercent} name="CPU usage, last hour" />
        </div>
        <div className={styles.chartBlock}>
          <BlockTitle title="Memory and swap" lesson="memory" topic="memory and swap" />
          <Now assessment={health.memory} />
          <Now assessment={health.swap} />
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
        </div>
        <div className={styles.chartBlock}>
          <BlockTitle title="Load" lesson="load" topic="load average" />
          <Now assessment={health.load} />
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
        </div>
        <div className={styles.chartBlock}>
          <BlockTitle title="Network" lesson="network" topic="network traffic" />
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
      </div>

      <div className={styles.tables}>
        <section className={styles.block} aria-label="Disks">
          <BlockTitle title="Disks" lesson="disk" topic="disks" />
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
                        <Meter.Indicator className={styles.indicator} data-level={assessPercent("disk", d.usedPercent).level} />
                      </Meter.Track>
                    </Meter.Root>
                  )}
                  <span className={`${styles.diskText} ${toneClass}`} data-level={assessPercent('disk', d.usedPercent).level}>
                    {d.usedPercent === null ? UNKNOWN : formatPercent(d.usedPercent)}
                    <LevelMark assessment={assessPercent('disk', d.usedPercent, `disk ${d.mount}`)} />
                    <span className={styles.muted}> {d.usedBytes === null || d.totalBytes === null ? '' : `${formatBytes(d.usedBytes)} / ${formatBytes(d.totalBytes)}`}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        {containers.length > 0 && (
          <section className={styles.block} aria-label="Containers">
            <BlockTitle title="Containers" lesson="containers" topic="containers" />
            <ul className={styles.list}>
              {containers.map((c) => (
                <li key={c.name} className={styles.container}>
                  <span className={styles.containerName}>
                    {c.name}
                    <span className={styles.muted}> {c.image}</span>
                  </span>
                  <span className={styles.containerText}>{c.cpuPercent === null ? UNKNOWN : formatPercent(c.cpuPercent)}</span>
                  <span className={`${styles.containerText} ${toneClass}`} data-level={assessPercent('containerMemory', c.memUsedPercent).level}>
                    {c.memUsedPercent === null ? UNKNOWN : formatPercent(c.memUsedPercent)}
                    <LevelMark assessment={assessPercent('containerMemory', c.memUsedPercent, `${c.name} memory`)} />
                    <span className={styles.muted}> {c.memUsedBytes === null ? '' : formatBytes(c.memUsedBytes)}</span>
                  </span>
                </li>
              ))}
            </ul>
          </section>
        )}

        <section className={styles.block} aria-label="Network interfaces">
          <BlockTitle title="Interfaces" lesson="network" topic="network interfaces" />
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
          <BlockTitle title="Checks" lesson="checks" topic="checks" />
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
          <BlockTitle title="Events" lesson="events" topic="host events" />
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
