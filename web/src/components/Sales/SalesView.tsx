import { useState } from 'react'
import shared from '../Dashboard/Dashboard.module.css'
import { formatAge, formatPercent } from '../../domain/format'
import {
  dayRows,
  formatDay,
  formatRubles,
  npdLevel,
  planNames,
  problems,
  SUMMARY_STALE_MS,
  totals,
  type BusinessDocument,
  type SalesRange,
} from '../../domain/sales'
import styles from './SalesView.module.css'

const RANGES: SalesRange[] = [7, 30, 90]

const PLAN_LABEL: Record<string, string> = { month: 'month', year: 'year' }

interface SalesViewProps {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  documents: BusinessDocument[]
  now: number
  onRetry: () => void
}

/**
 * The Sales view (docs/SPEC.md §5, Change 27): the tracked site's own summary of orders, payments,
 * refunds and sign-ups, as its host agent last reported it. Counts and sums only.
 */
export function SalesView({ status, error, documents, now, onRetry }: SalesViewProps) {
  if (status === 'loading')
    return (
      <div className={shared.skeleton} role="status">
        <span className="visually-hidden">Loading sales…</span>
        <div className={shared.skeletonRow} aria-hidden="true" />
        <div className={shared.skeletonRow} aria-hidden="true" />
      </div>
    )
  if (status === 'error')
    return (
      <section className={shared.section} role="alert">
        <p className={shared.notice}>
          <span className={shared.noticeStrong}>Could not load sales.</span> {error}{' '}
          <button type="button" className={shared.button} onClick={onRetry}>
            retry
          </button>
        </p>
      </section>
    )
  if (documents.length === 0)
    return (
      <section className={shared.section} aria-labelledby="sales-title">
        <h2 id="sales-title" className={shared.sectionTitle}>
          Sales
        </h2>
        <p className={shared.notice}>
          No summary has arrived yet. The site writes it hourly; its host agent reports it when started with{' '}
          <code className={styles.code}>--business-file</code>.
        </p>
      </section>
    )
  return (
    <>
      {error && (
        <p className={shared.notice} role="alert">
          <span className={shared.noticeStrong}>Data may be out of date.</span> The last refresh failed ({error}).
        </p>
      )}
      {documents.map((doc) => (
        <SiteSales key={doc.host_id} doc={doc} now={now} />
      ))}
    </>
  )
}

function SiteSales({ doc, now }: { doc: BusinessDocument; now: number }) {
  const [range, setRange] = useState<SalesRange>(30)
  const summary = doc.body
  const rows = dayRows(summary, range)
  const sum = totals(rows)
  const plans = planNames(summary)
  const age = now - new Date(summary.generated_at).getTime()
  const stale = age > SUMMARY_STALE_MS
  const issues = problems(summary)
  const share = summary.npd.limit_kopecks > 0 ? summary.npd.income_kopecks / summary.npd.limit_kopecks : 0
  const maxKopecks = Math.max(0, ...rows.map((r) => r.kopecks))
  const titleId = `sales-${doc.host_id}`
  const flags = [
    summary.billing_enabled ? 'payments on' : 'payments off',
    summary.paywall_enabled ? 'paywall on' : 'paywall off',
  ].join(' · ')

  return (
    <section className={styles.site} aria-labelledby={titleId}>
      <div className={styles.head}>
        <h2 id={titleId} className={styles.title}>
          Sales
        </h2>
        <p className={styles.source} data-state={stale ? 'stale' : 'ok'}>
          <span className={styles.pulse} aria-hidden="true" />
          <span>
            {doc.host_name} · summary {formatAge(age)}{stale ? ' — stale, the hourly job or the agent has stopped' : ''}
          </span>
          <span className={styles.muted}>{flags}</span>
        </p>
        <div className={styles.rangeToggle} role="group" aria-label="Period">
          {RANGES.map((r) => (
            <button key={r} type="button" className={styles.rangeButton} data-active={range === r} onClick={() => setRange(r)}>
              {r}d
            </button>
          ))}
        </div>
      </div>

      <dl className={styles.measures}>
        <Measure label={`Revenue ${range}d`} value={formatRubles(sum.kopecks)} />
        <Measure label={`Paid ${range}d`} value={`${sum.paid}`} hint={`of ${sum.orders} orders`} />
        <Measure label="Orders → paid" value={sum.conversion === null ? '—' : formatPercent(sum.conversion * 100)} />
        <Measure label="Active access" value={`${summary.access.active}`} />
        <Measure label="Ending in 7d" value={`${summary.access.ending_7d}`} />
      </dl>

      <div className={styles.status}>
        <div className={styles.npd} data-level={npdLevel(share)}>
          <span className={styles.label}>НПД {summary.npd.year}</span>
          <span>
            {formatRubles(summary.npd.income_kopecks)} of {formatRubles(summary.npd.limit_kopecks)} ·{' '}
            {formatPercent(share * 100)}
          </span>
          <span
            className={styles.meter}
            role="meter"
            aria-label={`НПД limit used, ${summary.npd.year}`}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(share * 100)}
          >
            <span className={styles.meterFill} style={{ width: `${Math.min(100, share * 100)}%` }} />
          </span>
        </div>
        {issues.length > 0 ? (
          <ul className={styles.problems} aria-label="Payment problems">
            {issues.map((p) => (
              <li key={p.text} className={styles.problem} data-level={p.level}>
                <span className={styles.pulse} aria-hidden="true" />
                {p.text}
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.muted}>No payment problems.</p>
        )}
        <p className={styles.muted}>
          {summary.reconcile
            ? `Provider check ${formatAge(now - new Date(summary.reconcile.checked_at).getTime())}: ${summary.reconcile.checked} payments, ${summary.reconcile.mismatches} differ.`
            : 'No provider check yet.'}
        </p>
      </div>

      <section className={styles.block} aria-labelledby={`${titleId}-days`}>
        <h3 id={`${titleId}-days`} className={shared.sectionTitle}>
          By day
        </h3>
        <div className={styles.scroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Day</th>
                {plans.map((p) => (
                  <th key={p} scope="col" className={styles.num}>
                    {PLAN_LABEL[p] ?? p}
                  </th>
                ))}
                <th scope="col" className={styles.num}>
                  Refunded
                </th>
                <th scope="col" className={styles.num}>
                  Revenue
                </th>
                <th scope="col" className={styles.barHead}>
                  <span className="visually-hidden">Revenue scale</span>
                </th>
                <th scope="col" className={styles.num}>
                  Sign-ups
                </th>
                <th scope="col" className={styles.num}>
                  Confirmed
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.day} data-empty={r.orders === 0 && r.accounts === 0}>
                  <th scope="row">{formatDay(r.day)}</th>
                  {plans.map((p) => {
                    const cell = r.plans[p]
                    return (
                      <td key={p} className={styles.num} title="paid / orders">
                        {cell ? `${cell.paid} / ${cell.orders}` : '0 / 0'}
                      </td>
                    )
                  })}
                  <td className={styles.num}>{r.refunded}</td>
                  <td className={styles.num}>{formatRubles(r.kopecks)}</td>
                  <td className={styles.barCell} aria-hidden="true">
                    <span className={styles.bar} style={{ width: maxKopecks > 0 ? `${(r.kopecks / maxKopecks) * 100}%` : '0%' }} />
                  </td>
                  <td className={styles.num}>{r.accounts}</td>
                  <td className={styles.num}>{r.confirmed}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <th scope="row">Total</th>
                {plans.map((p) => {
                  const paid = rows.reduce((n, r) => n + (r.plans[p]?.paid ?? 0), 0)
                  const orders = rows.reduce((n, r) => n + (r.plans[p]?.orders ?? 0), 0)
                  return (
                    <td key={p} className={styles.num}>
                      {paid} / {orders}
                    </td>
                  )
                })}
                <td className={styles.num}>{sum.refunded}</td>
                <td className={styles.num}>{formatRubles(sum.kopecks)}</td>
                <td />
                <td className={styles.num}>{sum.accounts}</td>
                <td className={styles.num}>{sum.confirmed}</td>
              </tr>
            </tfoot>
          </table>
        </div>
        <p className={styles.muted}>
          Plans as paid / orders. Days in the site's time (Moscow). All time: {summary.accounts_total} accounts,{' '}
          {summary.paying_accounts_total} paying
          {summary.accounts_total > 0
            ? ` (${formatPercent((summary.paying_accounts_total / summary.accounts_total) * 100)} sign-up → payment)`
            : ''}
          .
        </p>
      </section>
    </section>
  )
}

function Measure({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className={styles.measure}>
      <dt className={styles.label}>{label}</dt>
      <dd className={styles.value}>
        {value}
        {hint && <span className={styles.hint}> {hint}</span>}
      </dd>
    </div>
  )
}
