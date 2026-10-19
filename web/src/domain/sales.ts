// The tracked site's business summary (Change 27): counts and sums the site writes hourly
// (schema "kodumka.business/1") and its host agent reports as a document. Nothing here names a
// person. Days are the site's calendar days (Moscow), as the summary states them.

export interface FunnelLine {
  day: string
  plan: string
  orders: number
  paid: number
  failed: number
  open: number
  refunded: number
  kopecks: number
}

export interface RegistrationLine {
  day: string
  accounts: number
  confirmed: number
}

export interface BusinessSummary {
  schema: string
  generated_at: string
  billing_enabled: boolean
  paywall_enabled: boolean
  funnel: FunnelLine[]
  registrations: RegistrationLine[]
  accounts_total: number
  paying_accounts_total: number
  access: { active: number; ending_7d: number }
  npd: { year: number; income_kopecks: number; limit_kopecks: number }
  problems: {
    stuck_orders: number
    refunds_requested: number
    refunds_pending: number
    refunds_failed: number
  }
  reconcile: { checked_at: string; checked: number; mismatches: number } | null
}

export interface BusinessDocument {
  host_id: string
  host_name: string
  received_at: string
  body: BusinessSummary
}

export const SALES_SCHEMA = 'kodumka.business/1'

/** A summary is written hourly: older than two hours means the site's job or the agent stopped. */
export const SUMMARY_STALE_MS = 2 * 60 * 60_000

export type SalesRange = 7 | 30 | 90

export type Level = 'ok' | 'warn' | 'critical'

/** The last `days` calendar days, ending with the summary's own day, newest first. */
export function rangeDays(generatedAt: string, days: number): string[] {
  const end = new Date(`${moscowDay(generatedAt)}T00:00:00Z`)
  return Array.from({ length: days }, (_, i) => {
    const d = new Date(end)
    d.setUTCDate(end.getUTCDate() - i)
    return d.toISOString().slice(0, 10)
  })
}

/** The Moscow calendar day of an instant: the site counts days there. */
export function moscowDay(iso: string): string {
  return new Date(new Date(iso).getTime() + 3 * 60 * 60_000).toISOString().slice(0, 10)
}

export interface DayRow {
  day: string
  plans: Record<string, { orders: number; paid: number }>
  orders: number
  paid: number
  failed: number
  refunded: number
  kopecks: number
  accounts: number
  confirmed: number
}

export interface Totals {
  orders: number
  paid: number
  failed: number
  refunded: number
  kopecks: number
  accounts: number
  confirmed: number
  /** paid ÷ orders, or null without orders. */
  conversion: number | null
}

/** One row per calendar day of the range, newest first; a day without activity stays as zeros. */
export function dayRows(summary: BusinessSummary, days: SalesRange): DayRow[] {
  const rows = new Map<string, DayRow>(
    rangeDays(summary.generated_at, days).map((day) => [
      day,
      { day, plans: {}, orders: 0, paid: 0, failed: 0, refunded: 0, kopecks: 0, accounts: 0, confirmed: 0 },
    ]),
  )
  for (const line of summary.funnel) {
    const row = rows.get(line.day)
    if (!row) continue
    const plan = (row.plans[line.plan] ??= { orders: 0, paid: 0 })
    plan.orders += line.orders
    plan.paid += line.paid
    row.orders += line.orders
    row.paid += line.paid
    row.failed += line.failed
    row.refunded += line.refunded
    row.kopecks += line.kopecks
  }
  for (const line of summary.registrations) {
    const row = rows.get(line.day)
    if (!row) continue
    row.accounts += line.accounts
    row.confirmed += line.confirmed
  }
  return [...rows.values()]
}

export function totals(rows: DayRow[]): Totals {
  const t = rows.reduce(
    (acc, r) => ({
      orders: acc.orders + r.orders,
      paid: acc.paid + r.paid,
      failed: acc.failed + r.failed,
      refunded: acc.refunded + r.refunded,
      kopecks: acc.kopecks + r.kopecks,
      accounts: acc.accounts + r.accounts,
      confirmed: acc.confirmed + r.confirmed,
    }),
    { orders: 0, paid: 0, failed: 0, refunded: 0, kopecks: 0, accounts: 0, confirmed: 0 },
  )
  return { ...t, conversion: t.orders > 0 ? t.paid / t.orders : null }
}

/** Plans in a stable order: the summary's own names, month before year, then the rest. */
export function planNames(summary: BusinessSummary): string[] {
  const order = ['month', 'year']
  const names = [...new Set(summary.funnel.map((l) => l.plan))]
  return names.sort((a, b) => {
    const ia = order.indexOf(a)
    const ib = order.indexOf(b)
    return (ia < 0 ? order.length : ia) - (ib < 0 ? order.length : ib) || a.localeCompare(b)
  })
}

/** НПД yearly limit share: 70 % watch, 85 % problem (the site pauses new payments at 95 %). */
export function npdLevel(share: number): Level {
  if (share >= 0.85) return 'critical'
  if (share >= 0.7) return 'warn'
  return 'ok'
}

export interface Problem {
  level: Level
  text: string
}

/** Payment problems worth the operator's attention; an empty list means none. */
export function problems(summary: BusinessSummary): Problem[] {
  const p = summary.problems
  const out: Problem[] = []
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
  if (p.refunds_failed > 0) out.push({ level: 'critical', text: `${plural(p.refunds_failed, 'refund', 'refunds')} failed` })
  if (summary.reconcile && summary.reconcile.mismatches > 0)
    out.push({
      level: 'critical',
      text: `${plural(summary.reconcile.mismatches, 'payment differs', 'payments differ')} from the provider`,
    })
  if (p.stuck_orders > 0) out.push({ level: 'warn', text: `${plural(p.stuck_orders, 'order', 'orders')} stuck over 2 h` })
  if (p.refunds_pending > 0) out.push({ level: 'warn', text: `${plural(p.refunds_pending, 'refund', 'refunds')} pending at the provider` })
  if (p.refunds_requested > 0) out.push({ level: 'warn', text: `${plural(p.refunds_requested, 'refund', 'refunds')} waiting to be sent` })
  return out
}

/** "12 340 ₽" from kopecks; whole rubles, thin-space thousands. */
export function formatRubles(kopecks: number): string {
  const rubles = Math.round(kopecks / 100)
  return `${rubles.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ' ')} ₽`
}

/** "12 Oct" for a calendar day. */
export function formatDay(day: string): string {
  const d = new Date(`${day}T00:00:00Z`)
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', timeZone: 'UTC' })
}
