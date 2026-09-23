import { Popover } from '@base-ui/react/popover'
import { LESSON_BY_ID, type LessonId } from '../../guide/lessons'
import { useOpenLesson } from '../Guide/GuideNav'
import styles from './HelpButton.module.css'

interface HelpButtonProps {
  /** The lesson this block is explained by. */
  lesson: LessonId
  /** What the button explains, for its accessible name ("What is CPU?"). */
  topic: string
}

/**
 * "?" next to a section or block title (docs/SPEC.md §5, Change 22): a click/tap/keyboard popover
 * with the lesson's summary and assessment rule, and a way into the full lesson in the guide.
 */
export function HelpButton({ lesson, topic }: HelpButtonProps) {
  const openLesson = useOpenLesson()
  const l = LESSON_BY_ID[lesson]
  return (
    <Popover.Root>
      <Popover.Trigger className={styles.trigger} aria-label={`What is ${topic}?`}>
        ?
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner className={styles.positioner} sideOffset={6} collisionPadding={16}>
          <Popover.Popup className={styles.popup}>
            <Popover.Title className={styles.title}>{l.title}</Popover.Title>
            <Popover.Description className={styles.text}>{l.summary}</Popover.Description>
            {l.thresholds && <p className={styles.rule}>Assessment: {l.thresholds}.</p>}
            <div className={styles.actions}>
              <Popover.Close className={styles.action} onClick={() => openLesson(lesson)}>
                read the lesson →
              </Popover.Close>
              <Popover.Close className={styles.action}>close</Popover.Close>
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
