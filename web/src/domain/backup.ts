// Backup summaries (docs/SPEC.md §4.1, §5, Change 29): a host that runs backups writes a
// `smotryashchiy.backup/1` document and its agent reports it as kind `backup`. Counts, sizes and
// times only. Every rule below turns one fact into normal / watch / problem with a sentence that
// says why, the same way the rest of the dashboard is assessed (domain/health.ts).

import { formatAge, formatBytes, formatPercent } from './format'
import { THRESHOLDS, worst, type Assessment, type Level } from './health'

export const BACKUP_SCHEMA = 'smotryashchiy.backup/1'

export type JobStatus = 'success' | 'failed' | 'not-configured'

export interface BackupJob {
  name: string
  label: string
  status: JobStatus
  completed_at: string | null
  max_age_hours: number
  duration_seconds?: number | null
  source?: string | null
}

export interface BackupRepository {
  name: string
  snapshots: number
  size_bytes: number
}

export interface BackupHold {
  name: string
  created_at: string
  review_after_days: number
}

export interface BackupSummary {
  schema: string
  generated_at: string
  jobs: BackupJob[]
  repositories: BackupRepository[]
  holds: BackupHold[]
  disk: { path: string; free_bytes: number; total_bytes: number } | null
  failed_units: string[]
}

export interface BackupDocument {
  host_id: string
  host_name: string
  received_at: string
  body: unknown
}

const HOUR_MS = 60 * 60_000
const DAY_MS = 24 * HOUR_MS

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const isText = (v: unknown): v is string => typeof v === 'string'
const isCount = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0
const isTime = (v: unknown): v is string => isText(v) && !Number.isNaN(Date.parse(v))

function parseJob(v: unknown): BackupJob | null {
  if (!isObject(v) || !isText(v.name) || !isText(v.label) || !isCount(v.max_age_hours)) return null
  if (v.status !== 'success' && v.status !== 'failed' && v.status !== 'not-configured') return null
  if (v.completed_at !== null && !isTime(v.completed_at)) return null
  return {
    name: v.name,
    label: v.label,
    status: v.status,
    completed_at: v.completed_at,
    max_age_hours: v.max_age_hours,
    duration_seconds: isCount(v.duration_seconds) ? v.duration_seconds : null,
    source: isText(v.source) ? v.source : null,
  }
}

function parseList<T>(v: unknown, item: (x: unknown) => T | null): T[] | null {
  if (!Array.isArray(v)) return null
  const out: T[] = []
  for (const x of v) {
    const parsed = item(x)
    if (parsed === null) return null
    out.push(parsed)
  }
  return out
}

/**
 * The summary when the body is a well-formed `smotryashchiy.backup/1`, otherwise null: a summary in
 * another schema, or a malformed one, is shown as unknown and never half-read.
 */
export function parseBackupSummary(body: unknown): BackupSummary | null {
  if (!isObject(body) || body.schema !== BACKUP_SCHEMA || !isTime(body.generated_at)) return null
  const jobs = parseList(body.jobs, parseJob)
  const repositories = parseList(body.repositories, (r) =>
    isObject(r) && isText(r.name) && isCount(r.snapshots) && isCount(r.size_bytes)
      ? { name: r.name, snapshots: r.snapshots, size_bytes: r.size_bytes }
      : null,
  )
  const holds = parseList(body.holds, (h) =>
    isObject(h) && isText(h.name) && isTime(h.created_at) && isCount(h.review_after_days)
      ? { name: h.name, created_at: h.created_at, review_after_days: h.review_after_days }
      : null,
  )
  const failedUnits = parseList(body.failed_units, (u) => (isText(u) ? u : null))
  const d = body.disk
  const disk =
    d === null
      ? null
      : isObject(d) && isText(d.path) && isCount(d.free_bytes) && isCount(d.total_bytes) && d.total_bytes > 0
        ? { path: d.path, free_bytes: d.free_bytes, total_bytes: d.total_bytes }
        : undefined
  if (!jobs || !repositories || !holds || !failedUnits || disk === undefined) return null
  return {
    schema: body.schema,
    generated_at: body.generated_at,
    jobs,
    repositories,
    holds,
    disk,
    failed_units: failedUnits,
  }
}

/** The age of the summary itself: older than two hours means its writer or the agent stopped. */
export function assessSummaryAge(summary: BackupSummary, nowMs: number): Assessment {
  const age = nowMs - Date.parse(summary.generated_at)
  const limit = THRESHOLDS.backupSummaryHours
  if (age > limit * HOUR_MS)
    return {
      level: 'problem',
      reason: `summary written ${formatAge(age)} (problem > ${limit}h: the host's backup job or agent stopped)`,
    }
  return { level: 'normal', reason: `summary written ${formatAge(age)}` }
}

/** One job: failed, never completed or older than its own limit is a problem; not set up is watch. */
export function assessJob(job: BackupJob, nowMs: number): Assessment {
  if (job.status === 'not-configured') return { level: 'watch', reason: `${job.label}: not configured` }
  if (job.completed_at === null)
    return { level: 'problem', reason: `${job.label}: ${job.status === 'failed' ? 'failed, ' : ''}never completed` }
  const age = nowMs - Date.parse(job.completed_at)
  if (job.status === 'failed') return { level: 'problem', reason: `${job.label}: failed ${formatAge(age)}` }
  if (age > job.max_age_hours * HOUR_MS)
    return {
      level: 'problem',
      reason: `${job.label}: last success ${formatAge(age)} (problem > ${job.max_age_hours}h)`,
    }
  return { level: 'normal', reason: `${job.label}: ok ${formatAge(age)}` }
}

/** A protected snapshot kept past its review date should be looked at and released. */
export function assessHold(hold: BackupHold, nowMs: number): Assessment {
  const days = Math.floor((nowMs - Date.parse(hold.created_at)) / DAY_MS)
  if (days > hold.review_after_days)
    return {
      level: 'watch',
      reason: `hold ${hold.name} is ${days}d old (watch > ${hold.review_after_days}d: review and release it)`,
    }
  return { level: 'normal', reason: `hold ${hold.name}, ${days}d old` }
}

/** Free space where the backups are written; a full disk stops the next backup. */
export function assessBackupDisk(disk: BackupSummary['disk']): Assessment {
  if (disk === null) return { level: 'unknown', reason: 'backup disk not reported' }
  const free = (disk.free_bytes / disk.total_bytes) * 100
  const b = THRESHOLDS.backupDiskFree
  const text = `backup disk ${disk.path} ${formatPercent(free)} free (${formatBytes(disk.free_bytes)})`
  if (free < b.problem) return { level: 'problem', reason: `${text} (problem < ${b.problem}%)` }
  if (free < b.watch) return { level: 'watch', reason: `${text} (watch < ${b.watch}%)` }
  return { level: 'normal', reason: text }
}

export function assessFailedUnits(units: string[]): Assessment {
  if (units.length === 0) return { level: 'normal', reason: 'no failed backup units' }
  return { level: 'problem', reason: `failed unit${units.length === 1 ? '' : 's'}: ${units.join(', ')}` }
}

export interface HostBackup {
  hostId: string
  hostName: string
  /** Null when the document is not a readable `smotryashchiy.backup/1` summary. */
  summary: BackupSummary | null
  summaryAge: Assessment
  jobs: { job: BackupJob; assessment: Assessment }[]
  holds: { hold: BackupHold; assessment: Assessment }[]
  disk: Assessment
  units: Assessment
  overall: Level
  /** Watch and problem findings, worst first, for the ledger row and the summary line. */
  issues: Assessment[]
}

/** Everything the BACKUPS ledger and the summary line need to know about one host's backups. */
export function hostBackup(doc: BackupDocument, nowMs: number): HostBackup {
  const summary = parseBackupSummary(doc.body)
  const base = { hostId: doc.host_id, hostName: doc.host_name }
  if (!summary) {
    const unknown: Assessment = { level: 'unknown', reason: 'backup summary in an unknown format' }
    return {
      ...base,
      summary: null,
      summaryAge: unknown,
      jobs: [],
      holds: [],
      disk: unknown,
      units: unknown,
      overall: 'unknown',
      issues: [],
    }
  }
  const summaryAge = assessSummaryAge(summary, nowMs)
  const jobs = summary.jobs.map((job) => ({ job, assessment: assessJob(job, nowMs) }))
  const holds = summary.holds.map((hold) => ({ hold, assessment: assessHold(hold, nowMs) }))
  const disk = assessBackupDisk(summary.disk)
  const units = assessFailedUnits(summary.failed_units)
  const all = [summaryAge, ...jobs.map((j) => j.assessment), ...holds.map((h) => h.assessment), disk, units]
  // A summary that stopped arriving says nothing reliable about now, like an offline host.
  const overall = summaryAge.level === 'problem' ? 'problem' : worst(all.map((a) => a.level))
  const rank: Record<Level, number> = { problem: 0, watch: 1, normal: 2, unknown: 3 }
  const issues = all
    .filter((a) => a.level === 'watch' || a.level === 'problem')
    .sort((a, b) => rank[a.level] - rank[b.level])
  return { ...base, summary, summaryAge, jobs, holds, disk, units, overall, issues }
}

/**
 * The host's primary backup: the first job of the summary by contract (§4.1). The ledger row shows
 * it, not the newest of any job, so a passing weekly check cannot hide a failing nightly backup.
 */
export function primaryJob(host: HostBackup): HostBackup['jobs'][number] | null {
  return host.jobs[0] ?? null
}
