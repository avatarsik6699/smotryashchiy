import { Dialog } from '@base-ui/react/dialog'
import { useState } from 'react'
import { downloadCSV, useAnalyticsQuery } from '../../data/useAnalytics'
import { ALL_METRICS, displayValue, formatCount, METRICS, type Filter, type PeriodId } from '../../domain/analytics'
import shared from '../Dashboard/Dashboard.module.css'
import dialog from './AddSiteDialog.module.css'
import styles from './BreakdownPanel.module.css'

export interface PanelTab {
  id: string
  /** The dimension queried; a function when it depends on the filters (browser → its versions). */
  dimension: string | ((filters: Filter[]) => string)
  /** The column heading for that dimension. */
  column: string | ((filters: Filter[]) => string)
}

interface BreakdownPanelProps {
  siteId: string
  title: string
  tabs: PanelTab[]
  period: PeriodId
  filters: Filter[]
  onFilter: (dimension: string, value: string) => void
}

const TOP = 10

function resolve(v: string | ((filters: Filter[]) => string), filters: Filter[]): string {
  return typeof v === 'function' ? v(filters) : v
}

/** One breakdown panel: tabs, the top values by visitors with neutral share bars, details. */
export function BreakdownPanel({ siteId, title, tabs, period, filters, onFilter }: BreakdownPanelProps) {
  const [tabIndex, setTabIndex] = useState(0)
  const [details, setDetails] = useState(false)
  const tab = tabs[tabIndex] ?? tabs[0]!
  const dimension = resolve(tab.dimension, filters)
  const column = resolve(tab.column, filters)
  // a campaign tab lists campaigns, not the visits without one
  const hideEmpty = dimension.startsWith('utm_')
  const data = useAnalyticsQuery(siteId, { metrics: ['visitors'], dimension, period, filters, limit: TOP + 1 })
  const rows = (data.result?.rows ?? []).filter((r) => !hideEmpty || r.value !== '').slice(0, TOP)
  const max = Math.max(1, ...rows.map((r) => r.metrics.visitors ?? 0))

  return (
    <section className={styles.panel} aria-label={title}>
      <div className={styles.head}>
        <h3 className={styles.title}>{title}</h3>
        {tabs.length > 1 &&
          tabs.map((t, i) => (
            <button key={t.id} type="button" className={styles.tab} aria-pressed={i === tabIndex} onClick={() => setTabIndex(i)}>
              {t.id}
            </button>
          ))}
      </div>
      <div className={styles.columns} aria-hidden="true">
        <span>{column}</span>
        <span>visitors</span>
      </div>
      {data.status === 'error' && filters.length > 0 ? (
        // the dashboard's notice gives the reason once (filters need raw days)
        <p className={styles.empty}>not available with these filters</p>
      ) : data.status === 'error' ? (
        <p className={styles.empty} role="alert">
          Could not load ({data.error}).
        </p>
      ) : data.status === 'loading' ? (
        <p className={styles.empty} role="status">
          loading…
        </p>
      ) : rows.length === 0 ? (
        <p className={styles.empty}>no data</p>
      ) : (
        <ul className={styles.list}>
          {rows.map((r) => {
            const label = displayValue(dimension, r.value, filters)
            const visitors = r.metrics.visitors ?? 0
            return (
              <li key={r.value}>
                <button type="button" className={styles.row} title="Filter the dashboard by this value" onClick={() => onFilter(dimension, r.value)}>
                  <span className={styles.share} style={{ width: `${Math.round((visitors / max) * 100)}%` }} aria-hidden="true" />
                  <span className={styles.label}>
                    {label}
                    <span className="visually-hidden">, filter by this {column}</span>
                  </span>
                  <span className={styles.value}>{formatCount(visitors)}</span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
      <div className={styles.foot}>
        <button type="button" className={styles.details} onClick={() => setDetails(true)}>
          details
        </button>
      </div>
      {details && <DetailsDialog siteId={siteId} title={title} dimension={dimension} column={column} period={period} filters={filters} onClose={() => setDetails(false)} />}
    </section>
  )
}

interface DetailsDialogProps {
  siteId: string
  title: string
  dimension: string
  column: string
  period: PeriodId
  filters: Filter[]
  onClose: () => void
}

/** Up to 100 values with every metric, and the same query as a CSV file. */
function DetailsDialog({ siteId, title, dimension, column, period, filters, onClose }: DetailsDialogProps) {
  const query = { metrics: ALL_METRICS, dimension, period, filters, limit: 100 }
  const data = useAnalyticsQuery(siteId, query)
  const [failed, setFailed] = useState(false)
  const range = data.result?.date_range

  async function csv() {
    setFailed(false)
    try {
      await downloadCSV(siteId, query, `${dimension}-${range ? range.join('-') : period}.csv`)
    } catch {
      setFailed(true)
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={dialog.backdrop} />
        <Dialog.Popup className={`${dialog.popup} ${styles.dialog}`}>
          <Dialog.Title className={dialog.title}>
            {title}: {column}
          </Dialog.Title>
          <Dialog.Description className={dialog.description}>{range ? `${range[0]} – ${range[1]}, top 100 by visitors.` : 'Top 100 by visitors.'}</Dialog.Description>
          {data.status === 'error' && (
            <p className={dialog.error} role="alert">
              Could not load ({data.error}).
            </p>
          )}
          {data.status === 'loading' && (
            <p className={dialog.status} role="status">
              loading…
            </p>
          )}
          {data.result && (
            <div className={styles.tableBox} tabIndex={0} role="region" aria-label={`${title}: ${column}`}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th scope="col">{column}</th>
                    {ALL_METRICS.map((m) => (
                      <th key={m} scope="col" className={styles.num}>
                        {METRICS[m].label.toLowerCase()}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {data.result.rows.map((r) => (
                    <tr key={r.value}>
                      <th scope="row">{displayValue(dimension, r.value, filters)}</th>
                      {ALL_METRICS.map((m) => (
                        <td key={m} className={styles.num}>
                          {METRICS[m].format(r.metrics[m] ?? 0)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className={dialog.actions}>
            <button type="button" className={shared.button} onClick={() => void csv()}>
              Download CSV
            </button>
            <Dialog.Close className={shared.button}>Close</Dialog.Close>
          </div>
          <p className={dialog.status} role="status">
            {failed && 'Could not download the file. Try again.'}
          </p>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
