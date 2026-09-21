import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { useEffect, useMemo, useRef, useState } from 'react'
import { formatAxisTime, formatAxisTimeSeconds, formatClock, UNKNOWN } from '../../domain/format'
import type { Point } from '../../domain/types'
import { alignSeries, clampTooltip, sampleCount, summarize, yRange } from './layout'
import styles from './Chart.module.css'

export interface ChartSeries {
  label: string
  points: Point[]
  /** Timestamps (ms) where the series has no value, drawn as breaks in the line. */
  gaps?: number[]
}

export interface ChartProps {
  series: ChartSeries[]
  /** spark: a bare line for the ledger; full: axes, tooltip and a text summary. */
  variant: 'spark' | 'full'
  height: number
  /** percent: fixed 0-100 axis; auto: from zero to the data maximum. */
  kind: 'percent' | 'auto'
  format: (value: number) => string
  /** Accessible name, e.g. "CPU, last hour". */
  name: string
  /** A chart whose numbers are already present as text (e.g. inside a row button) is hidden from assistive tech. */
  decorative?: boolean
}

/**
 * Line chart on uPlot honoring the layout contract of docs/SPEC.md §5: the container clips and can
 * shrink, size follows a ResizeObserver, the built-in legend is off in favor of our own text, axis
 * sizes are measured from the real labels, and the tooltip is clamped inside the container.
 */
export function Chart({ series, variant, height, kind, format, name, decorative = false }: ChartProps) {
  const boxRef = useRef<HTMLDivElement>(null)
  const plotRef = useRef<HTMLDivElement>(null)
  const tipRef = useRef<HTMLDivElement>(null)
  const plotInstance = useRef<uPlot | null>(null)
  const [width, setWidth] = useState(0)
  const widthRef = useRef(0)
  const themeKey = useColorSchemeKey()

  const aligned = useMemo(() => alignSeries(series.map((s) => s.points), undefined, series.map((s) => s.gaps ?? [])), [series])
  const enough = sampleCount(series.map((s) => s.points)) >= 2
  const max = useMemo(() => Math.max(0, ...series.flatMap((s) => s.points.map((p) => p.v))), [series])
  const rangeRef = useRef<[number, number]>([0, 1])
  rangeRef.current = yRange(kind, max)
  const dataRef = useRef<uPlot.AlignedData>([[], []])
  dataRef.current = [aligned.xs, ...aligned.ys] as uPlot.AlignedData
  const heightRef = useRef(height)
  heightRef.current = height
  const formatRef = useRef(format)
  formatRef.current = format
  const labelsSig = series.map((s) => s.label).join('\u0000')
  const canDraw = enough && width > 0

  // Track the container's width. Zero (hidden or collapsed) means "do not draw yet".
  useEffect(() => {
    const box = boxRef.current
    if (!box) return
    const observer = new ResizeObserver((entries) => {
      const w = Math.floor(entries[0]?.contentRect.width ?? 0)
      widthRef.current = w
      // Resize the canvas right here, in the same frame as the container: waiting for a React render
      // would leave it wider than its container for a few frames while the window shrinks.
      if (w > 0) plotInstance.current?.setSize({ width: w, height: heightRef.current })
      setWidth(w)
    })
    observer.observe(box)
    return () => observer.disconnect()
  }, [])

  // Create the chart when it can be drawn; recreate when its shape or theme changes.
  useEffect(() => {
    const plot = plotRef.current
    const box = boxRef.current
    if (!canDraw || !plot || !box) return
    const theme = readTheme(box)
    const opts = buildOptions({ variant, height, width: widthRef.current, getRange: () => rangeRef.current, labels: labelsSig.split('\u0000'), theme, format: (v) => formatRef.current(v) })
    if (variant === 'full') {
      opts.hooks = { setCursor: [(u) => moveTooltip(u, box, tipRef.current, labelsSig.split('\u0000'), formatRef.current)] }
    }
    const u = new uPlot(opts, dataRef.current, plot)
    plotInstance.current = u
    return () => {
      u.destroy()
      if (plotInstance.current === u) plotInstance.current = null
      if (tipRef.current) tipRef.current.hidden = true
    }
  }, [canDraw, variant, height, kind, labelsSig, themeKey])

  useEffect(() => {
    const u = plotInstance.current
    if (u && width > 0) u.setSize({ width, height })
  }, [width, height])

  // New data goes in at most once per frame.
  useEffect(() => {
    const u = plotInstance.current
    if (!u) return
    const frame = requestAnimationFrame(() => {
      if (plotInstance.current === u) u.setData(dataRef.current, true)
    })
    return () => cancelAnimationFrame(frame)
  }, [aligned])

  const summaries = series.map((s) => summarize(s.points))
  const spark = variant === 'spark'
  // A spark inside a button must be phrasing-safe (no <figure>) and, being decorative, silent.
  const Root = spark ? 'div' : 'figure'
  const rootProps = decorative ? { 'aria-hidden': true } : spark ? { role: 'img', 'aria-label': name } : { 'aria-label': name }
  return (
    <Root className={spark ? styles.spark : styles.figure} {...rootProps}>
      <div ref={boxRef} className={styles.box} style={{ height }}>
        <div ref={plotRef} className={styles.plot} aria-hidden="true" />
        {!enough && (
          <p className={styles.empty} role="status">
            {spark ? UNKNOWN : 'no data yet'}
          </p>
        )}
        <div ref={tipRef} className={styles.tip} hidden aria-hidden="true" />
      </div>
      {!spark && (
        <figcaption className={styles.caption}>
          <ul className={styles.legend}>
            {series.map((s, i) => {
              const sum = summaries[i]!
              return (
                <li key={s.label} className={styles.legendItem}>
                  <span className={styles.swatch} data-style={String(i)} aria-hidden="true" />
                  <span className={styles.legendLabel}>{s.label}</span>
                  <span className={styles.legendValues}>
                    {sum.latest === null ? UNKNOWN : `${format(sum.latest)} now`}
                    {sum.min !== null && sum.max !== null ? ` · min ${format(sum.min)} · max ${format(sum.max)}` : ''}
                  </span>
                </li>
              )
            })}
          </ul>
        </figcaption>
      )}
    </Root>
  )
}

interface Theme {
  font: string
  accent: string
  text: string
  muted: string
  hairline: string
}

function readTheme(el: HTMLElement): Theme {
  const cs = getComputedStyle(el)
  const v = (name: string, fallback: string) => cs.getPropertyValue(name).trim() || fallback
  // Canvas fonts cannot take a var() list with fallbacks the way CSS does; use the first family.
  const family = v('--font', 'monospace').split(',')[0]!.trim().replace(/^['"]|['"]$/g, '')
  return { font: family, accent: v('--accent', '#8bb89d'), text: v('--text', '#d8e0df'), muted: v('--text-muted', '#7e8c8c'), hairline: v('--hairline', '#263137') }
}

interface BuildArgs {
  variant: 'spark' | 'full'
  height: number
  width: number
  /** Read lazily so the axis follows the data after setData without recreating the chart. */
  getRange: () => [number, number]
  labels: string[]
  theme: Theme
  format: (v: number) => string
}

/** Line styles differ by dash as well as by color, so series stay distinguishable without color. */
const DASHES: (number[] | undefined)[] = [undefined, [6, 4], [2, 3]]

function buildOptions(a: BuildArgs): uPlot.Options {
  const colors = [a.theme.accent, a.theme.text, a.theme.muted]
  const spark = a.variant === 'spark'
  const axisFont = `11px ${a.theme.font}, monospace`
  const measure = (u: uPlot, values: string[] | null | undefined, gap: number, tick: number) => {
    if (!values || values.length === 0) return 32
    u.ctx.font = axisFont
    let widest = 0
    for (const v of values) widest = Math.max(widest, u.ctx.measureText(v).width)
    return Math.ceil(widest / uPlot.pxRatio) + gap + tick
  }
  return {
    width: a.width,
    height: a.height,
    padding: spark ? [2, 2, 2, 2] : [8, 20, 0, 0], // room for the last x label and the top y label to overhang
    legend: { show: false },
    cursor: spark ? { show: false } : { points: { size: 6 }, drag: { x: false, y: false }, y: false },
    select: { show: false, left: 0, top: 0, width: 0, height: 0 },
    scales: { x: { time: true }, y: { range: () => a.getRange() } },
    axes: spark
      ? [{ show: false }, { show: false }]
      : [
          {
            stroke: a.theme.muted,
            font: axisFont,
            size: 28,
            gap: 4,
            space: 90,
            grid: { stroke: a.theme.hairline, width: 1 },
            ticks: { stroke: a.theme.hairline, width: 1, size: 4 },
            // Under ~10 minutes of data, minute-only labels would repeat ("18:02 18:02"); add seconds then.
            values: (u, vals) => {
              const span = (u.scales.x?.max ?? 0) - (u.scales.x?.min ?? 0)
              return vals.map((t) => (span < 600 ? formatAxisTimeSeconds(t * 1000) : formatAxisTime(t * 1000)))
            },
          },
          {
            stroke: a.theme.muted,
            font: axisFont,
            gap: 6,
            size: (u, values) => measure(u, values, 6, 0),
            grid: { stroke: a.theme.hairline, width: 1 },
            ticks: { show: false },
            values: (_u, vals) => vals.map((v) => a.format(v)),
          },
        ],
    series: [
      {},
      ...a.labels.map((label, i) => ({
        label,
        stroke: colors[i % colors.length]!,
        width: spark ? 1.5 : 1.75,
        dash: DASHES[i % DASHES.length],
        spanGaps: false,
        points: { show: false },
      })),
    ],
  }
}

function moveTooltip(u: uPlot, box: HTMLElement, tip: HTMLDivElement | null, labels: string[], format: (v: number) => string) {
  if (!tip) return
  const { idx, left, top } = u.cursor
  if (idx === null || idx === undefined || left === undefined || top === undefined || left < 0) {
    tip.hidden = true
    return
  }
  const t = u.data[0]?.[idx]
  const lines: string[] = []
  labels.forEach((label, i) => {
    const v = u.data[i + 1]?.[idx]
    if (v !== null && v !== undefined) lines.push(`${label} ${format(v)}`)
  })
  if (t === undefined || lines.length === 0) {
    tip.hidden = true
    return
  }
  tip.textContent = `${formatClock(t * 1000)}\n${lines.join('\n')}`
  tip.hidden = false
  const boxRect = box.getBoundingClientRect()
  const pos = clampTooltip(
    { left: u.over.offsetLeft + left, top: u.over.offsetTop + top },
    { width: tip.offsetWidth, height: tip.offsetHeight },
    { width: boxRect.width, height: boxRect.height },
  )
  tip.style.left = `${pos.left}px`
  tip.style.top = `${pos.top}px`
}

/** Bumps when the system color scheme changes so the canvas colors are re-read. */
function useColorSchemeKey(): number {
  const [key, setKey] = useState(0)
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setKey((k) => k + 1)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return key
}
