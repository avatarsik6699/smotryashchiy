import { Button } from '@base-ui/react/button'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useState } from 'react'
import { formatAge } from '../../domain/format'
import {
  accountName,
  ACCOUNT_SCHEMA,
  OPERATOR_SCHEMA,
  flagText,
  formatRubles,
  formatWhen,
  KIND_LABEL,
  REPORT_STALE_MS,
  type AccountCard,
  type AuditEntry,
  type Incident,
  type OperatorCommand,
  type OperatorDocument,
  type Signal,
} from '../../domain/operator'
import { useCommand } from '../../data/useOperator'
import dialog from '../Dashboard/AddHostDialog.module.css'
import shared from '../Dashboard/Dashboard.module.css'
import { HelpButton } from '../Help/HelpButton'
import { CommandDialog, commandErrorMessage, type CommandIntent } from './CommandDialog'
import { IncidentsSection } from './IncidentsSection'
import styles from './Operator.module.css'

interface OperatorViewProps {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  documents: OperatorDocument[]
  commands: OperatorCommand[]
  incidents: Incident[]
  audit: AuditEntry[]
  now: number
  onReload: () => void
}

/**
 * The operator console for the tracked site (docs/SPEC.md §4j, §5 «Operator»): signals of shared
 * accounts and the manual measures, refunds, the account card, incidents and the audit log.
 * Every action is a command the site's host runs and answers; nothing here acts on its own.
 */
export function OperatorView({ status, error, documents, commands, incidents, audit, now, onReload }: OperatorViewProps) {
  const [intent, setIntent] = useState<CommandIntent | null>(null)
  if (status === 'loading')
    return (
      <div className={shared.skeleton} role="status">
        <span className="visually-hidden">Loading the operator console…</span>
        <div className={shared.skeletonRow} aria-hidden="true" />
        <div className={shared.skeletonRow} aria-hidden="true" />
      </div>
    )
  if (status === 'error')
    return (
      <section className={shared.section} role="alert">
        <p className={shared.notice}>
          <span className={shared.noticeStrong}>Could not load the operator console.</span> {error}{' '}
          <button type="button" className={shared.button} onClick={onReload}>
            retry
          </button>
        </p>
      </section>
    )
  // a report in another schema is named as unknown, never misread as this one
  const doc = documents.find((d) => d.body?.schema === OPERATOR_SCHEMA)
  const foreign = doc ? undefined : documents[0]
  return (
    <div className={styles.view}>
      {error && (
        <p className={shared.notice} role="alert">
          <span className={shared.noticeStrong}>Data may be out of date.</span> The last refresh failed ({error}).
        </p>
      )}
      {doc ? (
        <SiteConsole doc={doc} now={now} onAct={setIntent} />
      ) : (
        <section className={shared.section} aria-labelledby="operator-title">
          <h2 id="operator-title" className={shared.sectionTitle}>
            Operator
          </h2>
          {foreign ? (
            <p className={shared.notice}>
              The report from {foreign.host_name} is in an unknown format ({String(foreign.body?.schema ?? 'no schema')}). This
              console reads <code>{OPERATOR_SCHEMA}</code>; update the console or the site so they match.
            </p>
          ) : (
            <p className={shared.notice}>
              No operator report has arrived yet. The site writes it every ten minutes; its host agent reports it when
            started with <code>--document operator=…</code> and answers commands with <code>--operator-command</code>.
            </p>
          )}
        </section>
      )}
      <IncidentsSection incidents={incidents} now={now} onChanged={onReload} />
      <CommandLog commands={commands} />
      <AuditLog entries={audit} />
      <CommandDialog intent={intent} onClose={() => setIntent(null)} onFinished={onReload} />
    </div>
  )
}

function SiteConsole({ doc, now, onAct }: { doc: OperatorDocument; now: number; onAct: (intent: CommandIntent) => void }) {
  const report = doc.body
  const age = now - new Date(report.generated_at).getTime()
  const stale = age > REPORT_STALE_MS
  const hostId = doc.host_id
  return (
    <>
      <section className={styles.block} aria-labelledby="operator-title">
        <div className={styles.head}>
          <h2 id="operator-title" className={styles.title}>
            Operator
          </h2>
          <HelpButton lesson="operator" topic="the operator console" />
          <p className={styles.source} data-state={stale ? 'stale' : 'ok'}>
            <span className={styles.pulse} aria-hidden="true" />
            <span>
              {doc.host_name} · report {formatAge(age)}
              {stale ? ' — stale, the ten-minute job or the agent has stopped' : ''}
            </span>
          </p>
        </div>
        <dl className={styles.measures}>
          <Measure label={`Signals ${report.signal_days}d`} value={report.signals.length} />
          <Measure label="Running measures" value={report.restrictions.length} />
          <Measure label="Open refunds" value={report.refunds.length} />
        </dl>
      </section>

      <section className={styles.block} aria-labelledby="signals-title">
        <h3 id="signals-title" className={shared.sectionTitle}>
          Signals
        </h3>
        {report.signals.length === 0 ? (
          <p className={styles.muted}>No account looks shared. Signals come from cities per day, devices pushed out and new devices; reading volume never raises one.</p>
        ) : (
          <div className={styles.scroll}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col">Why</th>
                  <th scope="col" className={styles.num}>
                    Cities/day
                  </th>
                  <th scope="col" className={styles.num}>
                    Pushed out
                  </th>
                  <th scope="col" className={styles.num}>
                    New devices
                  </th>
                  <th scope="col">Signed in now</th>
                  <th scope="col">Measure</th>
                  <th scope="col" className={styles.actionCell}>
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {report.signals.map((s) => (
                  <SignalRow key={s.user_id} signal={s} hostId={hostId} onAct={onAct} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className={styles.block} aria-labelledby="restrictions-title">
        <h3 id="restrictions-title" className={shared.sectionTitle}>
          Restrictions
        </h3>
        {report.restrictions.length === 0 ? (
          <p className={styles.muted}>No running measures.</p>
        ) : (
          <div className={styles.scroll}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col">Measure</th>
                  <th scope="col">Reason shown</th>
                  <th scope="col">Since</th>
                  <th scope="col">Until</th>
                  <th scope="col" className={styles.actionCell}>
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {report.restrictions.map((r) => {
                  const subject = accountName(r.email, r.user_id)
                  return (
                    <tr key={r.id}>
                      <th scope="row">{subject}</th>
                      <td>{r.kind}</td>
                      <td>{r.reason}</td>
                      <td className={styles.nowrap}>{formatWhen(r.created_at)}</td>
                      <td className={styles.nowrap}>{r.ends_at ? formatWhen(r.ends_at) : 'no end'}</td>
                      <td className={styles.actionCell}>
                        <span className={styles.rowActions}>
                          <Button
                            className={shared.button}
                            aria-label={`Lift the ${r.kind} of ${subject}`}
                            onClick={() => onAct({ hostId, kind: 'lift', args: { user: r.user_id }, subject })}
                          >
                            lift
                          </Button>
                        </span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className={styles.block} aria-labelledby="refunds-title">
        <h3 id="refunds-title" className={shared.sectionTitle}>
          Refunds
        </h3>
        {report.refunds.length === 0 ? (
          <p className={styles.muted}>No refund waits for the operator.</p>
        ) : (
          <div className={styles.scroll}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col" className={styles.num}>
                    Order
                  </th>
                  <th scope="col" className={styles.num}>
                    Amount
                  </th>
                  <th scope="col">Why</th>
                  <th scope="col">State</th>
                  <th scope="col">Asked</th>
                  <th scope="col" className={styles.actionCell}>
                    <span className="visually-hidden">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {report.refunds.map((r) => {
                  const subject = `${formatRubles(r.amount_kopecks)} to ${accountName(r.email, r.user_id)}`
                  return (
                    <tr key={r.id}>
                      <th scope="row">{accountName(r.email, r.user_id)}</th>
                      <td className={styles.num}>{r.invoice}</td>
                      <td className={styles.num}>{formatRubles(r.amount_kopecks)}</td>
                      <td>{r.reason}</td>
                      <td>{r.status}</td>
                      <td className={styles.nowrap}>{formatWhen(r.created_at)}</td>
                      <td className={styles.actionCell}>
                        <span className={styles.rowActions}>
                          {r.status !== 'pending' && (
                            <Button
                              className={shared.button}
                              aria-label={`Execute the refund of ${subject}`}
                              onClick={() => onAct({ hostId, kind: 'refund_execute', args: { refund: r.id }, subject })}
                            >
                              execute
                            </Button>
                          )}
                        </span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <AccountLookup hostId={hostId} onAct={onAct} />
    </>
  )
}

function SignalRow({ signal, hostId, onAct }: { signal: Signal; hostId: string; onAct: (intent: CommandIntent) => void }) {
  const subject = accountName(signal.email, signal.user_id)
  const c = signal.counters
  const evidence = [
    `${signal.flags.map(flagText).join(', ')}`,
    `${c.max_cities_per_day} cities in one day, ${c.evictions} sessions pushed out, ${c.new_devices} new devices`,
    ...signal.sessions.map((s) => `signed in: ${[s.city, s.ip, s.label].filter(Boolean).join(' · ')}`),
  ]
  const act = (kind: CommandIntent['kind']) => onAct({ hostId, kind, args: signal.email ? { email: signal.email } : { user: signal.user_id }, subject, evidence })
  return (
    <tr>
      <th scope="row">{subject}</th>
      <td>{signal.flags.map(flagText).join(', ')}</td>
      <td className={styles.num}>{c.max_cities_per_day}</td>
      <td className={styles.num}>{c.evictions}</td>
      <td className={styles.num}>{c.new_devices}</td>
      <td>
        <ul className={styles.sessions}>
          {signal.sessions.map((s, i) => (
            <li key={s.id ?? i}>{[s.city ?? 'city unknown', s.ip, s.label].filter(Boolean).join(' · ')}</li>
          ))}
        </ul>
      </td>
      <td>{signal.restriction ?? '—'}</td>
      <td className={styles.actionCell}>
        <span className={styles.rowActions}>
          <Button className={shared.button} aria-label={`Warn ${subject}`} onClick={() => act('warn')}>
            warn
          </Button>
          <Button className={shared.button} aria-label={`Suspend paid access of ${subject}`} onClick={() => act('suspend')}>
            suspend
          </Button>
          <Button className={shared.button} aria-label={`Terminate paid access of ${subject}`} onClick={() => act('terminate')}>
            terminate
          </Button>
        </span>
      </td>
    </tr>
  )
}

/** Finds one account by email through the site's `account_show` and shows its card. */
function AccountLookup({ hostId, onAct }: { hostId: string; onAct: (intent: CommandIntent) => void }) {
  const { command, error, pending, run } = useCommand()
  const done = command?.status === 'done' ? command : null
  const card = done && done.result?.schema === ACCOUNT_SCHEMA ? (done.result as unknown as AccountCard) : null
  return (
    <section className={styles.block} aria-labelledby="account-title">
      <h3 id="account-title" className={shared.sectionTitle}>
        Account
      </h3>
      <Form
        className={styles.lookup}
        onFormSubmit={(values: Record<string, unknown>) => {
          const email = typeof values.email === 'string' ? values.email.trim() : ''
          if (email) void run({ host_id: hostId, kind: 'account_show', args: { email } })
        }}
      >
        <Field.Root name="email" className={dialog.field}>
          <Field.Label className={dialog.label}>Email</Field.Label>
          <Field.Control type="email" required autoComplete="off" spellCheck={false} className={dialog.input} />
        </Field.Root>
        <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.button}>
          {pending ? 'looking up…' : 'look up'}
        </Button>
      </Form>
      {error && (
        <p className={dialog.error} role="alert">
          {commandErrorMessage(error)}
        </p>
      )}
      {command && command.status !== 'done' && (
        <p className={styles.state} data-state={command.status} role="status">
          <span className={styles.pulse} aria-hidden="true" />
          {command.status === 'failed' ? `failed: ${command.error ?? 'no detail'}` : command.status}
        </p>
      )}
      {done && !card && (
        <p className={dialog.error} role="alert">
          The site answered in an unknown format ({String(done.result?.schema ?? 'no schema')}); this console reads{' '}
          <code>{ACCOUNT_SCHEMA}</code>.
        </p>
      )}
      {card && <AccountCardView card={card} hostId={hostId} onAct={onAct} />}
    </section>
  )
}

function AccountCardView({ card, hostId, onAct }: { card: AccountCard; hostId: string; onAct: (intent: CommandIntent) => void }) {
  const a = card.account
  const subject = accountName(a.email, a.id)
  const target = { user: a.id }
  return (
    <div className={styles.card}>
      <dl className={styles.facts}>
        <dt>Account</dt>
        <dd>
          {a.email ?? 'no email'} {a.email && !a.email_verified ? '(not confirmed)' : ''} · {a.id}
        </dd>
        <dt>Created</dt>
        <dd>{formatWhen(a.created_at)}</dd>
        <dt>Sign-in</dt>
        <dd>{a.methods.join(', ') || '—'}</dd>
        <dt>Paid access</dt>
        <dd>{card.access.until ? `until ${formatWhen(card.access.until)}` : 'none'}</dd>
        <dt>Payments</dt>
        <dd>
          {card.payments.length === 0
            ? 'none'
            : card.payments.map((p) => `#${p.invoice} ${p.plan} ${formatRubles(p.amount_kopecks)} ${p.status}`).join('; ')}
        </dd>
        <dt>Measures</dt>
        <dd>
          {card.restrictions.length === 0
            ? 'none'
            : card.restrictions.map((r) => `${r.kind} ${formatWhen(r.created_at)}${r.lifted_at ? ' (lifted)' : ''} — ${r.reason}`).join('; ')}
        </dd>
      </dl>
      <div className={`${styles.rowActions} ${styles.start}`}>
        <Button className={shared.button} onClick={() => onAct({ hostId, kind: 'warn', args: target, subject })}>
          warn
        </Button>
        <Button className={shared.button} onClick={() => onAct({ hostId, kind: 'suspend', args: target, subject })}>
          suspend
        </Button>
        <Button className={shared.button} onClick={() => onAct({ hostId, kind: 'terminate', args: target, subject })}>
          terminate
        </Button>
        <Button className={shared.button} onClick={() => onAct({ hostId, kind: 'lift', args: target, subject })}>
          lift
        </Button>
      </div>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <caption className="visually-hidden">Sessions signed in now</caption>
          <thead>
            <tr>
              <th scope="col">Signed in now</th>
              <th scope="col">City</th>
              <th scope="col">IP</th>
              <th scope="col">Since</th>
              <th scope="col">Last seen</th>
            </tr>
          </thead>
          <tbody>
            {card.sessions.length === 0 ? (
              <tr>
                <td colSpan={5} className={styles.detail}>
                  no active session
                </td>
              </tr>
            ) : (
              card.sessions.map((s, i) => (
                <tr key={s.id ?? i}>
                  <th scope="row">{s.label}</th>
                  <td>{s.city ?? '—'}</td>
                  <td>{s.ip ?? '—'}</td>
                  <td className={styles.nowrap}>{formatWhen(s.signed_in_at)}</td>
                  <td className={styles.nowrap}>{formatWhen(s.last_seen_at)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <caption className="visually-hidden">Activity by day</caption>
          <thead>
            <tr>
              <th scope="col">Day</th>
              <th scope="col">Cities</th>
              <th scope="col" className={styles.num}>
                New devices
              </th>
              <th scope="col" className={styles.num}>
                Pushed out
              </th>
              <th scope="col" className={styles.num}>
                Paid files
              </th>
              <th scope="col" className={styles.num}>
                Solutions
              </th>
            </tr>
          </thead>
          <tbody>
            {card.activity.length === 0 ? (
              <tr>
                <td colSpan={6} className={styles.detail}>
                  no activity in 30 days
                </td>
              </tr>
            ) : (
              card.activity.map((d) => (
                <tr key={d.day}>
                  <th scope="row" className={styles.nowrap}>
                    {d.day}
                  </th>
                  <td>{d.cities.join(', ') || '—'}</td>
                  <td className={styles.num}>{d.new_devices}</td>
                  <td className={styles.num}>{d.evictions}</td>
                  <td className={styles.num}>{d.paid_chunks}</td>
                  <td className={styles.num}>{d.solutions}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <p className={styles.muted}>Paid files and solutions are numbers for context only: they never raise a signal or limit a reader.</p>
    </div>
  )
}

function CommandLog({ commands }: { commands: OperatorCommand[] }) {
  return (
    <section className={styles.block} aria-labelledby="commands-title">
      <h3 id="commands-title" className={shared.sectionTitle}>
        Commands
      </h3>
      {commands.length === 0 ? (
        <p className={styles.muted}>No command has been sent yet.</p>
      ) : (
        <div className={styles.scroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Sent</th>
                <th scope="col">Command</th>
                <th scope="col">About</th>
                <th scope="col">State</th>
              </tr>
            </thead>
            <tbody>
              {commands.map((c) => (
                <tr key={c.id}>
                  <td className={styles.nowrap}>{formatWhen(c.created_at)}</td>
                  <td>{KIND_LABEL[c.kind] ?? c.kind}</td>
                  <td className={styles.detail}>{String(c.args.email ?? c.args.user ?? c.args.refund ?? '—')}</td>
                  <td>
                    <span className={styles.state} data-state={c.status}>
                      <span className={styles.pulse} aria-hidden="true" />
                      {c.status}
                      {c.status === 'failed' && c.error ? `: ${c.error}` : ''}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function AuditLog({ entries }: { entries: AuditEntry[] }) {
  return (
    <section className={styles.block} aria-labelledby="audit-title">
      <h3 id="audit-title" className={shared.sectionTitle}>
        Audit
      </h3>
      {entries.length === 0 ? (
        <p className={styles.muted}>Nothing recorded yet.</p>
      ) : (
        <div className={styles.scroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">When</th>
                <th scope="col">Action</th>
                <th scope="col">About</th>
                <th scope="col">Outcome</th>
                <th scope="col">From</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id}>
                  <td className={styles.nowrap}>{formatWhen(e.at)}</td>
                  <td>{e.action}</td>
                  <td className={styles.detail}>{e.target || '—'}</td>
                  <td>{e.outcome || '—'}</td>
                  <td className={styles.detail}>{e.client || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className={styles.muted}>The log cannot be edited; entries are kept for three years.</p>
    </section>
  )
}

function Measure({ label, value }: { label: string; value: number }) {
  return (
    <div className={styles.measure}>
      <dt className={styles.label}>{label}</dt>
      <dd className={styles.value}>{value}</dd>
    </div>
  )
}
