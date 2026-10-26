import type { SiteDTO } from '../../domain/types'
import shared from '../Dashboard/Dashboard.module.css'
import { HelpButton } from '../Help/HelpButton'
import { SiteDashboard } from './SiteDashboard'
import styles from './AnalyticsView.module.css'

interface AnalyticsViewProps {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  sites: SiteDTO[]
  onAddSite: () => void
}

/** The Analytics view (docs/SPEC.md §5): one site's dashboard (Change 37), a site picker on top. */
export function AnalyticsView({ status, error, sites, onAddSite }: AnalyticsViewProps) {
  return (
    <>
      {status === 'loading' && (
        <div className={shared.skeleton} role="status">
          <span className="visually-hidden">Loading sites…</span>
          <div className={shared.skeletonRow} aria-hidden="true" />
        </div>
      )}
      {status === 'error' && (
        <section className={shared.section} role="alert">
          <p className={shared.notice}>
            <span className={shared.noticeStrong}>Could not load sites.</span> {error}
          </p>
        </section>
      )}
      {status === 'ready' &&
        (sites.length === 0 ? (
          <section className={shared.section} aria-labelledby="sites-title">
            <div className={shared.titleRow}>
              <h2 id="sites-title" className={shared.sectionTitle}>
                Sites
              </h2>
              <HelpButton lesson="analytics" topic="site analytics" />
            </div>
            <p className={shared.notice}>
              No sites yet.{' '}
              <button type="button" className={styles.link} onClick={onAddSite}>
                Add a site
              </button>{' '}
              to get its tracking snippet.
            </p>
          </section>
        ) : (
          <section aria-label="Site analytics">
            <SiteDashboard sites={sites} />
          </section>
        ))}
    </>
  )
}
