// The operator console (docs/SPEC.md §4j, Change 30). The tracked site reports `kodumka.operator/1`
// (accounts above a sharing threshold, running measures, open refunds) and answers
// `account_show` with `kodumka.account/1`. Unlike the sales summary these name people (email,
// account ID, IP, city): the architect decided so on 2026-10-24 for the operator's own server.

export const OPERATOR_SCHEMA = 'kodumka.operator/1'
export const ACCOUNT_SCHEMA = 'kodumka.account/1'

/** The site writes its report every ten minutes; older than 30 minutes means it stopped. */
export const REPORT_STALE_MS = 30 * 60_000

export interface ActiveSession {
  id?: string
  label: string
  city: string | null
  ip: string | null
  last_seen_at: string | null
  signed_in_at?: string | null
}

export interface Signal {
  user_id: string
  email: string | null
  flags: string[]
  counters: {
    max_cities_per_day: number
    evictions: number
    new_devices: number
    max_paid_chunks_per_day: number
    max_solutions_per_day: number
  }
  sessions: ActiveSession[]
  restriction: string | null
}

export interface Restriction {
  id: string
  kind: 'warning' | 'suspension' | 'termination'
  reason: string
  created_at: string
  ends_at: string | null
  lifted_at: string | null
  user_id?: string
  email?: string | null
}

export interface OpenRefund {
  id: string
  invoice: number
  user_id: string | null
  email: string | null
  amount_kopecks: number
  reason: string
  status: string
  created_at: string
}

export interface OperatorReport {
  schema: string
  generated_at: string
  signal_days: number
  signals: Signal[]
  restrictions: Restriction[]
  refunds: OpenRefund[]
}

export interface OperatorDocument {
  host_id: string
  host_name: string
  received_at: string
  body: OperatorReport
}

export interface AccountCard {
  schema: string
  generated_at: string
  account: {
    id: string
    email: string | null
    email_verified: boolean
    created_at: string
    methods: string[]
    consent_version: string | null
  }
  access: { until: string | null }
  payments: {
    id: string
    invoice: number
    plan: string
    amount_kopecks: number
    status: string
    paid_at: string | null
    access_from: string | null
    access_until: string | null
  }[]
  refunds: { id: string; invoice: number; amount_kopecks: number; reason: string; status: string; created_at: string }[]
  sessions: ActiveSession[]
  devices: { label: string; first_seen_at: string; last_seen_at: string }[]
  restrictions: Restriction[]
  activity: { day: string; new_devices: number; evictions: number; cities: string[]; paid_chunks: number; solutions: number }[]
}

export type CommandKind = 'warn' | 'suspend' | 'terminate' | 'lift' | 'refund_execute' | 'account_show'
export type CommandStatus = 'queued' | 'running' | 'done' | 'failed' | 'expired'

export interface OperatorCommand {
  id: string
  host_id: string
  kind: CommandKind
  args: Record<string, unknown>
  reason?: string
  status: CommandStatus
  created_at: string
  started_at: string | null
  finished_at: string | null
  result: Record<string, unknown> | null
  error?: string
}

export const FINISHED: CommandStatus[] = ['done', 'failed', 'expired']

/** Measures the student sees a reason for; the server and the site require it. */
export function needsReason(kind: CommandKind): boolean {
  return kind === 'warn' || kind === 'suspend' || kind === 'terminate'
}

/** Destructive commands ask for the admin password again (step-up). */
export function needsPassword(kind: CommandKind): boolean {
  return kind === 'terminate' || kind === 'refund_execute'
}

export const KIND_LABEL: Record<CommandKind, string> = {
  warn: 'Warn',
  suspend: 'Suspend paid access',
  terminate: 'Terminate paid access',
  lift: 'Lift',
  refund_execute: 'Execute refund',
  account_show: 'Look up account',
}

const FLAG_TEXT: Record<string, string> = {
  cities: 'several cities in one day',
  evictions: 'devices pushed out',
  new_devices: 'many new devices',
}

/** A signal flag in words; unknown flags are shown as they are. */
export function flagText(flag: string): string {
  return FLAG_TEXT[flag] ?? flag
}

export interface Incident {
  id: string
  title: string
  kind: 'leak' | 'outage' | 'other'
  detected_at: string
  scope: string
  people_affected: number | null
  measures: string
  rkn_primary_at: string | null
  rkn_final_at: string | null
  users_notified_at: string | null
  notes: string
  closed_at: string | null
  created_at: string
  updated_at: string
}

export interface AuditEntry {
  id: number
  at: string
  action: string
  target: string
  detail: Record<string, unknown>
  client: string
  outcome: string
}

/** RKN deadlines for a personal-data leak (152-ФЗ ст. 21 ч. 3.1), counted from detection. */
export const RKN_PRIMARY_MS = 24 * 60 * 60_000
export const RKN_FINAL_MS = 72 * 60 * 60_000

export type DeadlineState = 'done' | 'open' | 'soon' | 'overdue'

export interface Deadline {
  label: string
  due: number
  /** Milliseconds left; negative when overdue. */
  left: number
  state: DeadlineState
}

/** The two RKN deadlines of a leak, or none for other incidents. A deadline is `soon` in its last
 * quarter, `overdue` past due, `done` once its notice is recorded. */
export function rknDeadlines(incident: Incident, now: number): Deadline[] {
  if (incident.kind !== 'leak') return []
  const detected = new Date(incident.detected_at).getTime()
  const make = (label: string, span: number, doneAt: string | null): Deadline => {
    const due = detected + span
    const left = due - now
    let state: DeadlineState = 'open'
    if (doneAt) state = 'done'
    else if (left < 0) state = 'overdue'
    else if (left < span / 4) state = 'soon'
    return { label, due, left, state }
  }
  return [
    make('primary RKN notice (24 h)', RKN_PRIMARY_MS, incident.rkn_primary_at),
    make('final RKN notice (72 h)', RKN_FINAL_MS, incident.rkn_final_at),
  ]
}

/** `3 h 05 min left`, `overdue by 2 h 10 min`. */
export function formatLeft(ms: number): string {
  const abs = Math.abs(ms)
  const hours = Math.floor(abs / 3_600_000)
  const minutes = Math.floor((abs % 3_600_000) / 60_000)
  const text = hours > 0 ? `${hours} h ${String(minutes).padStart(2, '0')} min` : `${minutes} min`
  return ms >= 0 ? `${text} left` : `overdue by ${text}`
}

/** What to do after a leak, in the site's memo order (privacy-legal-checklist §5). */
export const LEAK_CHECKLIST = [
  'Stop the leak: change passwords and keys, close the hole, keep the logs.',
  'Within 24 hours: primary notice to RKN at pd.rkn.gov.ru — what happened, which data and how many people, what was done, a contact.',
  'Within 72 hours: the final notice with the results of the internal investigation.',
  'Tell the affected people by email what happened and what to do.',
  'Write the timeline into the notes of this incident.',
]

const DATE_TIME = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })

/** `24 Oct, 14:05` in the viewer's time zone; `—` when there is no time. */
export function formatWhen(iso: string | null | undefined): string {
  return iso ? DATE_TIME.format(new Date(iso)) : '—'
}

/** Kopecks as rubles with a narrow no-break space: 49 000 → «490 ₽». */
export function formatRubles(kopecks: number): string {
  return `${new Intl.NumberFormat('ru-RU').format(Math.round(kopecks / 100))} ₽`
}

/** The account a row is about: its email when known, otherwise the start of its ID. */
export function accountName(email: string | null | undefined, userId: string | null | undefined): string {
  if (email) return email
  return userId ? `account ${userId.slice(0, 8)}` : 'deleted account'
}
