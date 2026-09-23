import { useId, useState } from 'react'
import { useSiteStats } from '../../data/useSiteStats'
import type { SiteDTO, StatsRange } from '../../domain/types'
import styles from './SiteDetails.module.css'

const RANGES: { value: StatsRange; label: string }[] = [
  { value: 'today', label: 'today' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

interface SiteDetailsProps {
  site: SiteDTO
}

/**
 * A site's full detail: the range toggle is the one exception to "the window is fixed"
 * (docs/SPEC.md §5) — a day-granularity domain has no sensible single fixed window.
 */
export function SiteDetails({ site }: SiteDetailsProps) {
  const [range, setRange] = useState<StatsRange>('today')
  const data = useSiteStats(site.id, range)
  const utcNoteId = useId()

  return (
    <div className={styles.details}>
      <div className={styles.rangeBar}>
        <div className={styles.rangeToggle} role="group" aria-label="Time range" aria-describedby={utcNoteId}>
          {RANGES.map((r) => (
            <button key={r.value} type="button" className={styles.rangeButton} data-active={range === r.value} onClick={() => setRange(r.value)}>
              {r.label}
            </button>
          ))}
        </div>
        {/* Ranges are UTC calendar days (docs/SPEC.md §4i); east of UTC "today" is not the local day. */}
        <span id={utcNoteId} className={styles.muted}>
          days in UTC
        </span>
      </div>

      {data.status === 'loading' && (
        <p className={styles.notice} role="status">
          loading…
        </p>
      )}
      {data.status === 'error' && (
        <p className={styles.notice} role="alert">
          Could not load stats ({data.error}).
        </p>
      )}
      {data.status === 'ready' && data.stats && (
        <>
          <div className={styles.summary}>
            <span className={styles.summaryItem}>
              <span className={styles.summaryLabel}>Pageviews</span>
              <span className={styles.summaryValue}>{data.stats.pageviews}</span>
            </span>
            <span className={styles.summaryItem}>
              <span className={styles.summaryLabel}>Visitors</span>
              <span className={styles.summaryValue}>{data.stats.visitors}</span>
            </span>
          </div>

          <div className={styles.tables}>
            <section className={styles.block} aria-label="Top pages">
              <h3 className={styles.blockTitle}>Top pages</h3>
              {data.stats.top_pages.length === 0 ? (
                <p className={styles.muted}>no data</p>
              ) : (
                <ul className={styles.list}>
                  {data.stats.top_pages.map((p) => (
                    <li key={p.path} className={styles.row}>
                      <span className={styles.rowLabel}>{p.path}</span>
                      <span className={styles.muted}>{p.count}</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            <section className={styles.block} aria-label="Top referrers">
              <h3 className={styles.blockTitle}>Top referrers</h3>
              {data.stats.top_referrers.length === 0 ? (
                <p className={styles.muted}>no data</p>
              ) : (
                <ul className={styles.list}>
                  {data.stats.top_referrers.map((r) => (
                    <li key={r.domain} className={styles.row}>
                      <span className={styles.rowLabel}>{r.domain}</span>
                      <span className={styles.muted}>{r.count}</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        </>
      )}
    </div>
  )
}
