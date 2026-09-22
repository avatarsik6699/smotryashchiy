import { Accordion } from '@base-ui/react/accordion'
import { useSiteStats } from '../../data/useSiteStats'
import type { SiteDTO } from '../../domain/types'
import { UNKNOWN } from '../../domain/format'
import { SiteDetails } from './SiteDetails'
import styles from './SiteRow.module.css'

interface SiteRowProps {
  site: SiteDTO
}

/** One site: name, domain and today's pageviews/visitors; expands in place to the full detail. */
export function SiteRow({ site }: SiteRowProps) {
  const today = useSiteStats(site.id, 'today')
  const pageviews = today.status === 'ready' ? String(today.stats?.pageviews ?? 0) : UNKNOWN
  const visitors = today.status === 'ready' ? String(today.stats?.visitors ?? 0) : UNKNOWN

  return (
    <Accordion.Item value={site.id} className={styles.item}>
      <Accordion.Header className={styles.header}>
        <Accordion.Trigger className={styles.trigger}>
          <svg className={styles.chevron} width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" aria-hidden="true">
            <path d="M3 1.5 6.5 5 3 8.5" />
          </svg>
          <span className={styles.identity}>
            <span className={styles.name}>{site.name}</span>
            <span className={styles.domain}>{site.domain}</span>
          </span>
          <span className={styles.metric}>
            <span className={styles.metricLabel}>PAGEVIEWS</span>
            <span className={styles.metricValue}>{pageviews}</span>
          </span>
          <span className={styles.metric}>
            <span className={styles.metricLabel}>VISITORS</span>
            <span className={styles.metricValue}>{visitors}</span>
          </span>
        </Accordion.Trigger>
      </Accordion.Header>
      <Accordion.Panel className={styles.panel}>
        <SiteDetails site={site} />
      </Accordion.Panel>
    </Accordion.Item>
  )
}
