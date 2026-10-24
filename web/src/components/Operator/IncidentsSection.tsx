import { Button } from '@base-ui/react/button'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import { formatLeft, formatWhen, LEAK_CHECKLIST, rknDeadlines, type Incident } from '../../domain/operator'
import dialog from '../Dashboard/AddHostDialog.module.css'
import shared from '../Dashboard/Dashboard.module.css'
import styles from './Operator.module.css'

type Mark = 'rkn_primary_at' | 'rkn_final_at' | 'users_notified_at' | 'closed_at'

const MARKS: { field: Mark; label: string; leakOnly: boolean }[] = [
  { field: 'rkn_primary_at', label: 'primary RKN notice sent', leakOnly: true },
  { field: 'rkn_final_at', label: 'final RKN notice sent', leakOnly: true },
  { field: 'users_notified_at', label: 'people notified', leakOnly: true },
  { field: 'closed_at', label: 'closed', leakOnly: false },
]

function incidentError(e: unknown): string {
  if (e instanceof ApiError && e.status === 400) return `${e.message.charAt(0).toUpperCase()}${e.message.slice(1)}.`
  if (e instanceof ApiError && e.kind === 'network') return 'Cannot reach the server. Check the connection and try again.'
  return 'Could not save the incident. Try again.'
}

/** The editable fields of an incident, as the API takes them for a full update. */
function inputOf(i: Incident): Record<string, unknown> {
  return {
    title: i.title,
    kind: i.kind,
    detected_at: i.detected_at,
    scope: i.scope,
    people_affected: i.people_affected,
    measures: i.measures,
    rkn_primary_at: i.rkn_primary_at,
    rkn_final_at: i.rkn_final_at,
    users_notified_at: i.users_notified_at,
    notes: i.notes,
    closed_at: i.closed_at,
  }
}

/**
 * Incidents (docs/SPEC.md §4j): what happened, the two RKN deadlines of a personal-data leak with a
 * live countdown, and what has been done. Filing with RKN stays manual; this keeps the clock.
 */
export function IncidentsSection({ incidents, now, onChanged }: { incidents: Incident[]; now: number; onChanged: () => void }) {
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const [formKey, setFormKey] = useState(0)
  const [confirming, setConfirming] = useState<string | null>(null)

  async function create(values: Record<string, unknown>) {
    const text = (key: string) => (typeof values[key] === 'string' ? (values[key] as string).trim() : '')
    const detected = text('detected_at')
    const people = text('people_affected')
    setPending(true)
    setError(null)
    try {
      await api('/api/operator/incidents', {
        method: 'POST',
        body: {
          title: text('title'),
          kind: text('kind') || 'leak',
          detected_at: detected ? new Date(detected).toISOString() : undefined,
          scope: text('scope'),
          people_affected: people === '' ? null : Number(people),
          measures: text('measures'),
        },
      })
      setFormKey((k) => k + 1)
      onChanged()
    } catch (e) {
      setError(incidentError(e))
    } finally {
      setPending(false)
    }
  }

  async function mark(incident: Incident, field: Mark) {
    setError(null)
    try {
      await api(`/api/operator/incidents/${incident.id}`, {
        method: 'PATCH',
        body: { ...inputOf(incident), [field]: new Date().toISOString() },
      })
      onChanged()
    } catch (e) {
      setError(incidentError(e))
    }
  }

  async function remove(incident: Incident) {
    setConfirming(null)
    setError(null)
    try {
      await api(`/api/operator/incidents/${incident.id}`, { method: 'DELETE' })
      onChanged()
    } catch (e) {
      setError(incidentError(e))
    }
  }

  return (
    <section className={styles.block} aria-labelledby="incidents-title">
      <h3 id="incidents-title" className={shared.sectionTitle}>
        Incidents
      </h3>
      {incidents.length === 0 ? (
        <p className={styles.muted}>No incident recorded.</p>
      ) : (
        <div className={styles.scroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Incident</th>
                <th scope="col">Detected</th>
                <th scope="col">Deadlines</th>
                <th scope="col">Done</th>
                <th scope="col" className={styles.actionCell}>
                  <span className="visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {incidents.map((i) => {
                const deadlines = i.closed_at ? [] : rknDeadlines(i, now)
                return (
                  <tr key={i.id}>
                    <th scope="row">
                      {i.title}
                      <span className={styles.detail}>
                        {' '}
                        · {i.kind}
                        {i.people_affected !== null ? ` · ${i.people_affected} people` : ''}
                        {i.scope ? ` · ${i.scope}` : ''}
                      </span>
                    </th>
                    <td className={styles.nowrap}>{formatWhen(i.detected_at)}</td>
                    <td>
                      {deadlines.length === 0 ? (
                        <span className={styles.detail}>{i.closed_at ? `closed ${formatWhen(i.closed_at)}` : 'none'}</span>
                      ) : (
                        <ul className={styles.deadlines}>
                          {deadlines.map((d) => (
                            <li key={d.label} className={styles.state} data-state={d.state}>
                              <span className={styles.pulse} aria-hidden="true" />
                              {d.label}: {d.state === 'done' ? 'sent' : formatLeft(d.left)}
                            </li>
                          ))}
                        </ul>
                      )}
                    </td>
                    <td className={styles.detail}>
                      {MARKS.filter((m) => i[m.field])
                        .map((m) => `${m.label} ${formatWhen(i[m.field])}`)
                        .join('; ') || '—'}
                    </td>
                    <td className={styles.actionCell}>
                      <span className={styles.rowActions}>
                        {MARKS.filter((m) => !i[m.field] && (!m.leakOnly || i.kind === 'leak') && !i.closed_at).map((m) => (
                          <Button key={m.field} className={shared.button} onClick={() => void mark(i, m.field)}>
                            {m.label}
                          </Button>
                        ))}
                        {confirming === i.id ? (
                          <>
                            <Button className={shared.button} onClick={() => void remove(i)}>
                              confirm delete
                            </Button>
                            <Button className={shared.button} onClick={() => setConfirming(null)}>
                              keep
                            </Button>
                          </>
                        ) : (
                          <Button className={shared.button} aria-label={`Delete the incident ${i.title}`} onClick={() => setConfirming(i.id)}>
                            delete
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
      <details>
        <summary className={styles.muted}>After a personal-data leak</summary>
        <ol className={styles.checklist}>
          {LEAK_CHECKLIST.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      </details>
      <Form key={formKey} className={styles.incidentForm} onFormSubmit={create} aria-label="Record an incident">
        <Field.Root name="title" className={dialog.field}>
          <Field.Label className={dialog.label}>What happened</Field.Label>
          <Field.Control required maxLength={200} autoComplete="off" className={dialog.input} />
        </Field.Root>
        <Field.Root name="kind" className={dialog.field}>
          <Field.Label className={dialog.label}>Kind</Field.Label>
          <Field.Control render={<select />} defaultValue="leak" className={`${dialog.input} ${styles.select}`}>
            <option value="leak">personal-data leak</option>
            <option value="outage">outage</option>
            <option value="other">other</option>
          </Field.Control>
        </Field.Root>
        <Field.Root name="detected_at" className={dialog.field}>
          <Field.Label className={dialog.label}>Detected (empty = now)</Field.Label>
          <Field.Control type="datetime-local" className={`${dialog.input} ${styles.select}`} />
        </Field.Root>
        <Field.Root name="people_affected" className={dialog.field}>
          <Field.Label className={dialog.label}>People affected</Field.Label>
          <Field.Control type="number" inputMode="numeric" min={0} className={dialog.input} />
        </Field.Root>
        <Field.Root name="scope" className={`${dialog.field} ${styles.wide}`}>
          <Field.Label className={dialog.label}>Which data</Field.Label>
          <Field.Control maxLength={10000} autoComplete="off" className={dialog.input} />
        </Field.Root>
        <Field.Root name="measures" className={`${dialog.field} ${styles.wide}`}>
          <Field.Label className={dialog.label}>Measures taken</Field.Label>
          <Field.Control render={<textarea />} maxLength={10000} className={`${dialog.input} ${styles.textarea}`} />
        </Field.Root>
        <p role="alert" className={`${dialog.error} ${styles.wide}`}>
          {error}
        </p>
        <span className={styles.wide}>
          <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.button}>
            {pending ? 'saving…' : '+ record incident'}
          </Button>
        </span>
      </Form>
    </section>
  )
}
