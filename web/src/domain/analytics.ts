// Site analytics dashboard (docs/SPEC.md §4i, §5; Change 37): the query API's vocabulary and the pure
// rules the view follows — metric formats, deltas against the comparison, labels for stored values.

export type MetricId = 'visitors' | 'visits' | 'pageviews' | 'views_per_visit' | 'bounce_rate' | 'visit_duration'
export type Interval = 'hour' | 'day' | 'week' | 'month'
export type PeriodId = 'today' | 'yesterday' | '7d' | '30d' | 'month' | 'last_month' | '12mo'
export type CompareId = '' | 'previous' | 'year_ago'

export interface Filter {
  dimension: string
  op: 'is' | 'is_not' | 'contains'
  values: string[]
}

export interface AnalyticsQuery {
  metrics?: MetricId[]
  dimension?: string
  filters?: Filter[]
  period?: PeriodId
  compare?: CompareId
  order_by?: MetricId
  limit?: number
  format?: 'json' | 'csv'
}

export interface ResultRowDTO {
  value: string
  metrics: Partial<Record<MetricId, number>>
  compare?: Partial<Record<MetricId, number>>
  compare_value?: string
}

export interface QueryResultDTO {
  date_range: [string, string]
  compare_range?: [string, string]
  timezone: string
  source: 'raw' | 'aggregates'
  rows: ResultRowDTO[]
}

export const ALL_METRICS: MetricId[] = ['visitors', 'visits', 'pageviews', 'views_per_visit', 'bounce_rate', 'visit_duration']

interface MetricInfo {
  label: string
  /** The bounce rate is the one metric where less is better. */
  higherIsBetter: boolean
  format: (v: number) => string
}

export const METRICS: Record<MetricId, MetricInfo> = {
  visitors: { label: 'Visitors', higherIsBetter: true, format: formatCount },
  visits: { label: 'Visits', higherIsBetter: true, format: formatCount },
  pageviews: { label: 'Pageviews', higherIsBetter: true, format: formatCount },
  views_per_visit: { label: 'Views per visit', higherIsBetter: true, format: (v) => v.toFixed(1) },
  bounce_rate: { label: 'Bounce rate', higherIsBetter: false, format: (v) => `${Math.round(v)}%` },
  visit_duration: { label: 'Visit duration', higherIsBetter: true, format: formatDuration },
}

/** A visitor hash lives one day, so over several days the visitors metric counts visitor-days. */
export function metricLabel(metric: MetricId, days: number): string {
  return metric === 'visitors' && days > 1 ? 'Visitor-days' : METRICS[metric].label
}

/** Groups thousands with a narrow no-break space: 12 480. */
export function formatCount(n: number): string {
  return Math.round(n)
    .toString()
    .replace(/\B(?=(\d{3})+(?!\d))/g, ' ')
}

export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

export interface Delta {
  /** "+18%", "−3 pp", "+0.2"; the minus is U+2212. */
  text: string
  direction: 'up' | 'down' | 'flat'
  /** Better or worse for this metric; null when flat. */
  better: boolean | null
}

/**
 * The change against the comparison. Counts and duration change in percent, the bounce rate in
 * percentage points, views per visit absolutely. Null when the comparison had nothing to compare.
 */
export function delta(metric: MetricId, current: number, previous: number | undefined): Delta | null {
  if (previous === undefined) return null
  let diff: number
  let text: string
  if (metric === 'bounce_rate') {
    diff = Math.round(current - previous)
    text = `${Math.abs(diff)} pp`
  } else if (metric === 'views_per_visit') {
    diff = Math.round((current - previous) * 10) / 10
    text = Math.abs(diff).toFixed(1)
  } else {
    if (previous === 0) return null
    diff = Math.round(((current - previous) / previous) * 100)
    text = `${Math.abs(diff)}%`
  }
  if (diff === 0) return { text: metric === 'views_per_visit' ? '0.0' : text, direction: 'flat', better: null }
  const up = diff > 0
  return { text: (up ? '+' : '−') + text, direction: up ? 'up' : 'down', better: up === METRICS[metric].higherIsBetter }
}

export const PERIODS: { id: PeriodId; label: string }[] = [
  { id: 'today', label: 'today' },
  { id: 'yesterday', label: 'yesterday' },
  { id: '7d', label: 'last 7 days' },
  { id: '30d', label: 'last 30 days' },
  { id: 'month', label: 'this month' },
  { id: 'last_month', label: 'last month' },
  { id: '12mo', label: 'last 12 months' },
]

export const COMPARES: { id: CompareId; label: string }[] = [
  { id: '', label: 'compare: off' },
  { id: 'previous', label: 'compare: previous period' },
  { id: 'year_ago', label: 'compare: a year ago' },
]

/** The intervals a period offers, the default first: hours for one day, months only for a year. */
export function intervalsFor(period: PeriodId): Interval[] {
  if (period === 'today' || period === 'yesterday') return ['hour', 'day']
  if (period === '12mo') return ['month', 'week', 'day']
  return ['day', 'week']
}

/** The number of days of a resolved range. */
export function rangeDays(range: [string, string]): number {
  return Math.round((Date.parse(range[1]) - Date.parse(range[0])) / 86_400_000) + 1
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

/** A bucket label: an hour's RFC 3339 start keeps its site clock time; the rest are site days. */
export function bucketLabel(interval: Interval, value: string, long = false): string {
  if (interval === 'hour') return value.slice(11, 16)
  const y = Number(value.slice(0, 4))
  const m = Number(value.slice(5, 7))
  const d = Number(value.slice(8, 10))
  if (interval === 'month') return `${MONTHS[m - 1]} ${y}`
  const weekday = WEEKDAYS[new Date(Date.UTC(y, m - 1, d)).getUTCDay()]
  const day = `${d} ${MONTHS[m - 1]}`
  if (interval === 'week') return long ? `week of ${weekday} ${day}` : day
  return long ? `${weekday} ${day}` : day
}

const CHANNELS: Record<string, string> = {
  paid_search: 'Paid search',
  organic_search: 'Organic search',
  paid_social: 'Paid social',
  social: 'Social',
  messengers: 'Messengers',
  ai_assistants: 'AI assistants',
  email: 'Email',
  referral: 'Referral',
  direct: 'Direct',
}

let regionNames: Intl.DisplayNames | null | undefined

function countryName(code: string): string {
  if (regionNames === undefined) {
    try {
      regionNames = new Intl.DisplayNames(['en'], { type: 'region' })
    } catch {
      regionNames = null
    }
  }
  try {
    return regionNames?.of(code) ?? code
  } catch {
    return code
  }
}

const VERSION_OF: Record<string, string> = { browser_version: 'browser', os_version: 'os' }

/**
 * How a stored dimension value reads; the empty value means "none" or "unknown" by dimension. A
 * version names its browser or OS when the filters say which one it is ("Chrome 131").
 */
export function displayValue(dimension: string, value: string, filters: Filter[] = []): string {
  const parent = VERSION_OF[dimension]
  if (parent) {
    const name = filters.find((f) => f.dimension === parent)?.values[0]
    if (name && value) return `${name} ${value}`
  }
  if (dimension === 'channel') return CHANNELS[value] ?? value
  if (value === '') {
    if (dimension === 'source') return 'Direct / none'
    if (dimension.startsWith('utm_')) return '(none)'
    return '(unknown)'
  }
  if (dimension === 'country') return countryName(value)
  return value
}
