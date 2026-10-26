import { useEffect, useId, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { bucketLabel, METRICS, type Interval, type MetricId, type ResultRowDTO } from '../../domain/analytics'
import styles from './TrendChart.module.css'

const HEIGHT = 240
const PAD = { top: 12, right: 12, bottom: 28, left: 52 }

interface TrendChartProps {
  rows: ResultRowDTO[]
  metric: MetricId
  /** The metric's label, e.g. "Visitor-days". */
  metricName: string
  interval: Interval
  comparing: boolean
  /** Accessible name, e.g. "Visitor-days per day, last 30 days, the comparison dashed". */
  name: string
}

/** 0 and three more ticks at a round step that covers the maximum. */
export function yTicks(max: number): number[] {
  if (max <= 0) return [0, 1, 2, 3]
  const raw = max / 3
  const pow = 10 ** Math.floor(Math.log10(raw))
  const step = ([1, 2, 2.5, 5, 10].find((m) => m * pow >= raw) ?? 10) * pow
  return [0, step, step * 2, step * 3]
}

/**
 * The period's trend (docs/SPEC.md §5, Change 37): the current period as a blue line over a light
 * area, the comparison as a grey dashed line, a crosshair and a tooltip by pointer or arrow keys.
 */
export function TrendChart({ rows, metric, metricName, interval, comparing, name }: TrendChartProps) {
  const boxRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(960)
  const [active, setActive] = useState<number | null>(null)
  const tipId = useId()

  useEffect(() => {
    const box = boxRef.current
    if (!box) return
    const observer = new ResizeObserver((entries) => {
      const w = Math.floor(entries[0]?.contentRect.width ?? 0)
      if (w > 0) setWidth(w)
    })
    observer.observe(box)
    return () => observer.disconnect()
  }, [])

  const format = METRICS[metric].format
  const cur = rows.map((r) => r.metrics[metric] ?? 0)
  const prev = rows.map((r) => r.compare?.[metric])
  const ticks = yTicks(Math.max(0, ...cur, ...prev.map((v) => v ?? 0)))
  const top = ticks[ticks.length - 1] ?? 1
  const plotW = Math.max(1, width - PAD.left - PAD.right)
  const plotH = HEIGHT - PAD.top - PAD.bottom
  const n = rows.length
  const x = (i: number) => PAD.left + (n <= 1 ? plotW / 2 : (i * plotW) / (n - 1))
  const y = (v: number) => PAD.top + plotH - (v / top) * plotH
  const path = (vs: (number | undefined)[]) => {
    let d = ''
    let pen = false
    vs.forEach((v, i) => {
      if (v === undefined) {
        pen = false
        return
      }
      d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)}`
      pen = true
    })
    return d
  }
  const line = path(cur)
  const area = n > 1 ? `${line}L${x(n - 1).toFixed(1)} ${y(0)}L${x(0).toFixed(1)} ${y(0)}Z` : ''
  const labelAt = n > 2 ? [0, Math.floor((n - 1) / 2), n - 1] : n === 2 ? [0, 1] : [0]

  function pick(e: PointerEvent<SVGSVGElement>) {
    if (n === 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    const px = ((e.clientX - rect.left) / rect.width) * width
    const i = n <= 1 ? 0 : Math.round(((px - PAD.left) / plotW) * (n - 1))
    setActive(Math.min(n - 1, Math.max(0, i)))
  }

  function key(e: KeyboardEvent<SVGSVGElement>) {
    if (n === 0) return
    const move = { ArrowLeft: -1, ArrowRight: 1, Home: -n, End: n }[e.key]
    if (e.key === 'Escape') {
      setActive(null)
      return
    }
    if (move === undefined) return
    e.preventDefault()
    setActive((a) => Math.min(n - 1, Math.max(0, (a ?? n - 1) + move)))
  }

  const tip = active !== null && active < n ? rows[active] : null
  const tipLeft = active !== null ? Math.min(Math.max(x(active) + 12, 0), width - 230) : 0

  return (
    <div ref={boxRef} className={styles.box}>
      <svg
        className={styles.svg}
        width={width}
        height={HEIGHT}
        viewBox={`0 0 ${width} ${HEIGHT}`}
        role="img"
        aria-label={name}
        aria-describedby={tip ? tipId : undefined}
        tabIndex={0}
        onPointerMove={pick}
        onPointerLeave={() => setActive(null)}
        onFocus={() => setActive((a) => a ?? (n > 0 ? n - 1 : null))}
        onBlur={() => setActive(null)}
        onKeyDown={key}
      >
        {ticks.map((t, i) => (
          <g key={t}>
            <line className={i === 0 ? styles.baseline : styles.grid} x1={PAD.left} x2={width - PAD.right} y1={y(t)} y2={y(t)} />
            <text className={styles.axis} x={PAD.left - 8} y={y(t) + 4} textAnchor="end">
              {format(t)}
            </text>
          </g>
        ))}
        {area && <path className={styles.area} d={area} />}
        {comparing && <path className={styles.compare} d={path(prev)} />}
        <path className={styles.current} d={line} />
        {n === 1 && <circle className={styles.dot} cx={x(0)} cy={y(cur[0] ?? 0)} r={4} />}
        {labelAt.map((i, k) => (
          <text key={i} className={styles.axis} x={x(i)} y={HEIGHT - 8} textAnchor={n === 1 ? 'middle' : k === 0 ? 'start' : k === labelAt.length - 1 ? 'end' : 'middle'}>
            {rows[i] ? bucketLabel(interval, rows[i].value) : ''}
          </text>
        ))}
        {active !== null && tip && (
          <g aria-hidden="true">
            <line className={styles.cross} x1={x(active)} x2={x(active)} y1={PAD.top} y2={PAD.top + plotH} />
            <circle className={styles.dot} cx={x(active)} cy={y(cur[active] ?? 0)} r={5} />
          </g>
        )}
      </svg>
      {tip && (
        <div id={tipId} className={styles.tip} style={{ left: tipLeft }} role="status">
          <div className={styles.tipTitle}>{bucketLabel(interval, tip.value, true)}</div>
          <div>
            <span className={styles.keyCurrent} aria-hidden="true" /> {format(tip.metrics[metric] ?? 0)} {metricName.toLowerCase()}
          </div>
          {comparing && tip.compare && (
            <div>
              <span className={styles.keyCompare} aria-hidden="true" /> {format(tip.compare[metric] ?? 0)} previous
              {tip.compare_value ? ` (${bucketLabel(interval, tip.compare_value, true)})` : ''}
            </div>
          )}
        </div>
      )}
      <table className="visually-hidden">
        <caption>{name}</caption>
        <thead>
          <tr>
            <th scope="col">{interval}</th>
            <th scope="col">{metricName}</th>
            {comparing && <th scope="col">comparison</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.value}>
              <th scope="row">{bucketLabel(interval, r.value, true)}</th>
              <td>{format(r.metrics[metric] ?? 0)}</td>
              {comparing && <td>{r.compare ? format(r.compare[metric] ?? 0) : ''}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
