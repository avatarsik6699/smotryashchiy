import { createContext, useContext } from 'react'
import type { LessonId } from '../../guide/lessons'

/** Opens the guide at a lesson; provided by the dashboard, a no-op elsewhere (e.g. isolated tests). */
export const GuideNavContext = createContext<(lesson: LessonId) => void>(() => {})

export function useOpenLesson(): (lesson: LessonId) => void {
  return useContext(GuideNavContext)
}
