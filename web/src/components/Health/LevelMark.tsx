import type { Assessment } from '../../domain/health'
import styles from './Health.module.css'

/** Tone class for a value: colored for watch/problem, unchanged otherwise. Pair it with a LevelMark. */
export const toneClass = styles.tone

/**
 * The level written out next to a colored value ("watch", "problem"), so color is never the only
 * signal (WCAG); normal and unknown values need no marker. The reason is available on hover and to
 * screen readers.
 */
export function LevelMark({ assessment }: { assessment: Assessment }) {
  if (assessment.level !== 'watch' && assessment.level !== 'problem') return null
  return (
    <span className={styles.mark} data-level={assessment.level} title={assessment.reason}>
      {assessment.level}
      <span className="visually-hidden">: {assessment.reason}</span>
    </span>
  )
}
