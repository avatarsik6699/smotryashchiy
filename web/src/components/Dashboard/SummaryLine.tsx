import type { Summary } from '../../data/health'
import { HelpButton } from '../Help/HelpButton'
import styles from './SummaryLine.module.css'

/**
 * The first line of Monitoring (docs/SPEC.md §5, Change 22): the worst finding first, then the facts
 * it is based on. The verdict is text ("All good", "Watch", "Problem"); color only repeats it.
 */
export function SummaryLine({ summary }: { summary: Summary }) {
  return (
    <div className={styles.summary} data-level={summary.level} role="status" aria-label="Summary">
      <span className={styles.headline}>{summary.headline}:</span>
      {summary.issues.length > 0 && (
        <ul className={styles.list} aria-label="Findings">
          {summary.issues.map((issue) => (
            <li key={issue.text} className={styles.issue} data-level={issue.level}>
              {issue.text}
            </li>
          ))}
        </ul>
      )}
      <ul className={`${styles.list} ${styles.facts}`} aria-label="Based on">
        {summary.facts.map((f) => (
          <li key={f}>{f}</li>
        ))}
      </ul>
      <HelpButton lesson="daily-check" topic="the summary line" />
    </div>
  )
}
