// Lesson ids without the lesson text (Change 38): the dashboard and its "?" buttons need the ids at
// once, the text (guide/lessons.ts) only when a popover or the guide opens.

export const LESSON_IDS = [
  "how-it-works",
  "daily-check",
  "status",
  "cpu",
  "load",
  "memory",
  "disk",
  "network",
  "containers",
  "checks",
  "events",
  "uptime",
  "backups",
  "analytics",
  "operator",
  "triage",
] as const;

export type LessonId = (typeof LESSON_IDS)[number];
