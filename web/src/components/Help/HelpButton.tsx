import { Popover } from '@base-ui/react/popover'
import { useState } from 'react'
import type { LessonId } from '../../guide/lessonIds'
import type { Lesson } from '../../guide/lessons'
import { loadLessons } from '../../guide/loadLessons'
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
 * with the lesson's summary and assessment rule, and a way into the full lesson in the guide. The
 * lesson text loads when the popover first opens (Change 38).
 */
export function HelpButton({ lesson, topic }: HelpButtonProps) {
  const openLesson = useOpenLesson()
  const [l, setLesson] = useState<Lesson | null>(null)
  const load = (open: boolean) => {
    if (open && !l) void loadLessons().then((all) => setLesson(all[lesson]))
  }
  return (
    <Popover.Root onOpenChange={load}>
      <Popover.Trigger className={styles.trigger} aria-label={`What is ${topic}?`}>
        ?
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner className={styles.positioner} sideOffset={6} collisionPadding={16}>
          <Popover.Popup className={styles.popup}>
            <Popover.Title className={styles.title}>{l ? l.title : topic}</Popover.Title>
            <Popover.Description className={styles.text}>{l ? l.summary : 'loading…'}</Popover.Description>
            {l?.thresholds && <p className={styles.rule}>Assessment: {l.thresholds}.</p>}
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
