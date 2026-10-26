import type { Lesson, LessonId } from './lessons'

let pending: Promise<Record<LessonId, Lesson>> | null = null

/** The lesson texts, loaded once on first use outside the main bundle (Change 38). */
export function loadLessons(): Promise<Record<LessonId, Lesson>> {
  pending ??= import('./lessons').then((m) => m.LESSON_BY_ID)
  return pending
}
