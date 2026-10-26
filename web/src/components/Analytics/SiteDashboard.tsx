import { useState } from 'react'
import { useAnalyticsQuery, useRealtime } from '../../data/useAnalytics'
import {
  ALL_METRICS,
  COMPARES,
  delta,
  displayValue,
  intervalsFor,
  METRICS,
  metricLabel,
  PERIODS,
  rangeDays,
  type CompareId,
  type Filter,
  type Interval,
  type MetricId,
  type PeriodId,
  type QueryResultDTO,
} from '../../domain/analytics'
import type { SiteDTO } from '../../domain/types'
import { HelpButton } from '../Help/HelpButton'
import { BreakdownPanel, type PanelTab } from './BreakdownPanel'
import { TrendChart } from './TrendChart'
import styles from './SiteDashboard.module.css'

const hasFilter = (filters: Filter[], dimension: string) => filters.some((f) => f.dimension === dimension)

/** The four panels of the mockup; a browser or OS filter turns its tab into that one's versions. */
const PANELS: { title: string; tabs: PanelTab[] }[] = [
  {
    title: 'Sources',
    tabs: [
      { id: 'channels', dimension: 'channel', column: 'channel' },
      { id: 'sources', dimension: 'source', column: 'source' },
      { id: 'campaigns', dimension: 'utm_campaign', column: 'utm campaign' },
    ],
  },
  {
    title: 'Pages',
    tabs: [
      { id: 'top', dimension: 'page', column: 'page' },
      { id: 'entry', dimension: 'entry_page', column: 'entry page' },
      { id: 'exit', dimension: 'exit_page', column: 'exit page' },
    ],
  },
  {
    title: 'Locations',
    tabs: [
      { id: 'countries', dimension: 'country', column: 'country' },
      { id: 'regions', dimension: 'region', column: 'region' },
    ],
  },
  {
    title: 'Devices',
    tabs: [
      { id: 'screen', dimension: 'screen', column: 'screen' },
      {
        id: 'browser',
        dimension: (f) => (hasFilter(f, 'browser') ? 'browser_version' : 'browser'),
        column: (f) => (hasFilter(f, 'browser') ? 'browser version' : 'browser'),
      },
      {
        id: 'os',
        dimension: (f) => (hasFilter(f, 'os') ? 'os_version' : 'os'),
        column: (f) => (hasFilter(f, 'os') ? 'os version' : 'os'),
      },
    ],
  },
]

const DIMENSION_NAMES: Record<string, string> = {
  entry_page: 'entry page',
  exit_page: 'exit page',
  utm_campaign: 'utm campaign',
  browser_version: 'browser version',
  os_version: 'os version',
}

interface SiteDashboardProps {
  sites: SiteDTO[]
}

/**
 * One site's analytics (docs/SPEC.md §5, Change 37): toolbar, key figures against the comparison,
 * the trend of the picked figure, and four breakdown panels whose rows filter everything. Filters
 * and comparison live in this view only, not in the URL (architect decision 2026-10-26).
 */
export function SiteDashboard({ sites }: SiteDashboardProps) {
  const [siteId, setSiteId] = useState(sites[0]?.id ?? '')
  const site = sites.find((s) => s.id === siteId) ?? sites[0]!
  const [period, setPeriod] = useState<PeriodId>('30d')
  const [compare, setCompare] = useState<CompareId>('previous')
  const [filters, setFilters] = useState<Filter[]>([])
  const [metric, setMetric] = useState<MetricId>('visitors')
  const [intervalChoice, setIntervalChoice] = useState<Interval | null>(null)
  const intervals = intervalsFor(period)
  const interval = intervalChoice && intervals.includes(intervalChoice) ? intervalChoice : intervals[0]!
  const realtime = useRealtime(site.id)

  const totals = useAnalyticsQuery(site.id, { metrics: ALL_METRICS, period, compare, filters })
  const series = useAnalyticsQuery(site.id, { metrics: [metric], dimension: `time:${interval}`, period, compare, filters })
  const days = totals.result ? rangeDays(totals.result.date_range) : 1
  const comparing = compare !== '' && Boolean(totals.result?.compare_range)
  const periodLabel = PERIODS.find((p) => p.id === period)?.label ?? period

  function changeSite(id: string) {
    setSiteId(id)
    setFilters([])
  }

  function addFilter(dimension: string, value: string) {
    setFilters((fs) => [...fs.filter((f) => f.dimension !== dimension), { dimension, op: 'is', values: [value] }])
  }

  return (
    <div className={styles.dashboard}>
      <div className={styles.toolbar}>
        <label className={styles.selectBox}>
          <span className="visually-hidden">Site</span>
          <select className={styles.select} value={site.id} onChange={(e) => changeSite(e.target.value)}>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} · {s.domain}
              </option>
            ))}
          </select>
          <Chevron />
        </label>
        <label className={styles.selectBox}>
          <span className="visually-hidden">Period</span>
          <select className={styles.select} value={period} onChange={(e) => setPeriod(e.target.value as PeriodId)}>
            {PERIODS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
          <Chevron />
        </label>
        <label className={styles.selectBox}>
          <span className="visually-hidden">Comparison</span>
          <select className={styles.select} value={compare} onChange={(e) => setCompare(e.target.value as CompareId)}>
            {COMPARES.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
          <Chevron />
        </label>
        {filters.map((f) => (
          <span key={f.dimension} className={styles.chip}>
            <span className={styles.muted}>{DIMENSION_NAMES[f.dimension] ?? f.dimension} is</span>
            <span className={styles.chipValue}>{displayValue(f.dimension, f.values[0] ?? '', filters)}</span>
            <button
              type="button"
              className={styles.chipRemove}
              aria-label={`Remove filter ${DIMENSION_NAMES[f.dimension] ?? f.dimension} is ${displayValue(f.dimension, f.values[0] ?? '', filters)}`}
              onClick={() => setFilters((fs) => fs.filter((x) => x !== f))}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                <path d="M18 6 6 18" />
                <path d="m6 6 12 12" />
              </svg>
            </button>
          </span>
        ))}
        <span className={styles.live}>
          <span className={styles.liveDot} aria-hidden="true" />
          <span className={styles.liveValue}>{realtime ?? '…'}</span> current visitors
          <HelpButton lesson="analytics" topic="site analytics" />
        </span>
      </div>

      {totals.status === 'error' && (
        <p className={styles.notice} role="alert">
          <span className={styles.noticeStrong}>Could not load the figures.</span> {totals.error}
          {filters.length > 0 && (
            <>
              {' '}
              <button type="button" className={styles.link} onClick={() => setFilters([])}>
                remove filters
              </button>
            </>
          )}
        </p>
      )}

      <KpiRow result={totals.result} days={days} comparing={comparing} against={compare === 'year_ago' ? 'a year ago' : 'previous'} metric={metric} onPick={setMetric} />

      <section className={styles.trend} aria-label={`${metricLabel(metric, days)} per ${interval}`}>
        <div className={styles.trendHead}>
          <h3 className={styles.caption}>
            {metricLabel(metric, days)} per {interval}
          </h3>
          <span className={styles.key}>
            <span className={styles.keyCurrent} aria-hidden="true" />
            this period
          </span>
          {comparing && (
            <span className={styles.key}>
              <span className={styles.keyCompare} aria-hidden="true" />
              {compare === 'year_ago' ? 'a year ago' : 'previous period'}
            </span>
          )}
          <span className={styles.intervals} role="group" aria-label="Interval">
            {intervals.map((i) => (
              <button key={i} type="button" className={styles.tab} aria-pressed={i === interval} onClick={() => setIntervalChoice(i)}>
                {i}
              </button>
            ))}
          </span>
        </div>
        {series.status === 'error' && filters.length > 0 ? (
          <p className={styles.notice}>not available with these filters</p>
        ) : series.status === 'error' ? (
          <p className={styles.notice} role="alert">
            Could not load the trend ({series.error}).
          </p>
        ) : (
          <TrendChart
            rows={series.result?.rows ?? []}
            metric={metric}
            metricName={metricLabel(metric, days)}
            interval={interval}
            comparing={comparing}
            name={`${metricLabel(metric, days)} per ${interval}, ${periodLabel}${comparing ? ', the comparison dashed' : ''}`}
          />
        )}
      </section>

      <div className={styles.panels}>
        {PANELS.map((p) => (
          <BreakdownPanel key={`${site.id}-${p.title}`} siteId={site.id} title={p.title} tabs={p.tabs} period={period} filters={filters} onFilter={addFilter} />
        ))}
      </div>

      <p className={styles.footnote}>
        Days in {totals.result?.timezone ?? 'the site’s timezone'}. Visitors over several days are visitor-days: the visitor hash changes daily, so one person on three days counts three times. Click a
        row to filter every panel. Geography: <a href="https://db-ip.com">IP Geolocation by DB-IP</a>.
      </p>
    </div>
  )
}

function Chevron() {
  return (
    <svg className={styles.chevron} width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="m6 9 6 6 6-6" />
    </svg>
  )
}

interface KpiRowProps {
  result: QueryResultDTO | null
  days: number
  comparing: boolean
  /** What the deltas compare with: "previous" or "a year ago". */
  against: string
  metric: MetricId
  onPick: (m: MetricId) => void
}

/** Six figures; each picks the trend's metric. A delta's arrow follows its sign, its colour says better or worse. */
function KpiRow({ result, days, comparing, against, metric, onPick }: KpiRowProps) {
  const row = result?.rows[0]
  return (
    <section className={styles.kpis} aria-label="Key figures">
      {ALL_METRICS.map((m) => {
        const value = row?.metrics[m]
        const d = comparing && row && value !== undefined ? delta(m, value, row.compare?.[m]) : null
        return (
          <button key={m} type="button" className={styles.kpi} aria-pressed={m === metric} onClick={() => onPick(m)}>
            <span className={styles.caption}>{metricLabel(m, days)}</span>
            <span className={styles.kpiValue}>{value === undefined ? '…' : METRICS[m].format(value)}</span>
            {comparing && (
              <span className={styles.delta}>
                {d ? (
                  <>
                    {d.direction !== 'flat' && (
                      <svg className={styles.arrow} data-direction={d.direction} data-better={d.better} width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
                        <path d="M5 1 9 8H1Z" />
                      </svg>
                    )}
                    <span className={styles.deltaValue} data-better={d.better ?? undefined}>
                      {d.text}
                    </span>{' '}
                    vs {against}
                    {d.better !== null && <span className="visually-hidden">, {d.better ? 'better' : 'worse'}</span>}
                  </>
                ) : (
                  'no earlier data'
                )}
              </span>
            )}
          </button>
        )
      })}
    </section>
  )
}
