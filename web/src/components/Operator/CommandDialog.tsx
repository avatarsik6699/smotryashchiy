import { Button } from '@base-ui/react/button'
import { Dialog } from '@base-ui/react/dialog'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useRef } from 'react'
import { ApiError } from '../../api/client'
import { useCommand } from '../../data/useOperator'
import { KIND_LABEL, needsPassword, needsReason, type CommandKind } from '../../domain/operator'
import dialog from '../Dashboard/AddHostDialog.module.css'
import shared from '../Dashboard/Dashboard.module.css'
import styles from './Operator.module.css'

export interface CommandIntent {
  hostId: string
  kind: CommandKind
  /** The account or refund the command is about, as the host receives it. */
  args: Record<string, unknown>
  /** Whom it is about, in words. */
  subject: string
  /** The numbers that led here, repeated so the operator decides on evidence. */
  evidence?: string[]
}

const CONSEQUENCE: Record<CommandKind, string> = {
  warn: 'Ends every session of the account and emails the student. Paid access stays.',
  suspend: 'Closes paid access until the end date and emails the student. The account, progress and free materials stay; paid days are not extended.',
  terminate: 'Ends paid access for good, requests a refund of the unused days and emails the student.',
  lift: 'Lifts every running measure of the account and emails the student.',
  refund_execute: 'Sends the requested refund to the payment provider. Money returns to the card it came from.',
  account_show: 'Reads the account card on the site.',
}

export function commandErrorMessage(error: Error): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return `${error.message.charAt(0).toUpperCase()}${error.message.slice(1)}.`
    if (error.status === 400) return `The server refused the command: ${error.message}.`
    if (error.kind === 'network') return 'Cannot reach the server. Check the connection and try again.'
    return `The server answered with an error (${error.status}). Try again.`
  }
  return 'Could not send the command. Try again.'
}

interface CommandDialogProps {
  intent: CommandIntent | null
  onClose: () => void
  /** Called once the host answered, so the view reloads its report. */
  onFinished: () => void
}

/**
 * Confirms one operator command (docs/SPEC.md §4j): what it does, the evidence, the reason the
 * student will read, and the admin password for destructive kinds. It then waits for the host.
 */
export function CommandDialog({ intent, onClose, onFinished }: CommandDialogProps) {
  const { command, error, pending, run, reset } = useCommand()
  const reasonRef = useRef<HTMLTextAreaElement>(null)
  const open = intent !== null

  function handleOpenChange(next: boolean) {
    if (!next) {
      reset()
      onClose()
    }
  }

  async function submit(values: Record<string, unknown>) {
    if (!intent) return
    const text = (key: string) => (typeof values[key] === 'string' ? (values[key] as string).trim() : '')
    const args = { ...intent.args }
    if (intent.kind === 'suspend') args.days = Number(text('days'))
    if (intent.kind === 'lift') args.compensate = values.compensate === 'on' || values.compensate === true
    const finished = await run({
      host_id: intent.hostId,
      kind: intent.kind,
      args,
      reason: needsReason(intent.kind) ? text('reason') : undefined,
      password: needsPassword(intent.kind) ? text('password') : undefined,
    })
    if (finished && finished.status === 'done') onFinished()
  }

  const done = command?.status === 'done'
  const finished = command && ['done', 'failed', 'expired'].includes(command.status)
  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={dialog.backdrop} />
        <Dialog.Popup className={dialog.popup} initialFocus={reasonRef}>
          {intent && (
            <>
              <Dialog.Title className={dialog.title}>
                {KIND_LABEL[intent.kind]}: {intent.subject}
              </Dialog.Title>
              <Dialog.Description className={dialog.description}>{CONSEQUENCE[intent.kind]}</Dialog.Description>
              {intent.evidence && intent.evidence.length > 0 && (
                <ul className={styles.sessions} aria-label="Evidence">
                  {intent.evidence.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              )}
              {!command && (
                <Form className={dialog.form} onFormSubmit={submit}>
                  {needsReason(intent.kind) && (
                    <Field.Root name="reason" className={dialog.field}>
                      <Field.Label className={dialog.label}>Reason the student reads (Russian)</Field.Label>
                      <Field.Control
                        render={<textarea ref={reasonRef} />}
                        required
                        maxLength={300}
                        className={`${dialog.input} ${styles.textarea}`}
                        placeholder="входы из трёх городов одновременно"
                      />
                    </Field.Root>
                  )}
                  {intent.kind === 'suspend' && (
                    <Field.Root name="days" className={dialog.field}>
                      <Field.Label className={dialog.label}>Days, 1–30</Field.Label>
                      <Field.Control type="number" inputMode="numeric" required min={1} max={30} defaultValue={7} className={dialog.input} />
                    </Field.Root>
                  )}
                  {intent.kind === 'lift' && (
                    <label className={styles.state}>
                      <input type="checkbox" name="compensate" />
                      Appeal upheld: extend access by the suspended time
                    </label>
                  )}
                  {needsPassword(intent.kind) && (
                    <Field.Root name="password" className={dialog.field}>
                      <Field.Label className={dialog.label}>Admin password</Field.Label>
                      <Field.Control type="password" required autoComplete="current-password" className={dialog.input} />
                    </Field.Root>
                  )}
                  <p role="alert" className={dialog.error}>
                    {error ? commandErrorMessage(error) : null}
                  </p>
                  <div className={dialog.actions}>
                    <Dialog.Close className={shared.button}>Cancel</Dialog.Close>
                    <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.buttonPrimary}>
                      {pending ? 'Sending…' : KIND_LABEL[intent.kind]}
                    </Button>
                  </div>
                </Form>
              )}
              {command && (
                <div className={dialog.form}>
                  <p className={styles.state} data-state={command.status} role="status">
                    <span className={styles.pulse} aria-hidden="true" />
                    {command.status === 'queued' && 'queued — waiting for the host to pick it up'}
                    {command.status === 'running' && 'running on the host'}
                    {command.status === 'done' && 'done'}
                    {command.status === 'failed' && `failed: ${command.error ?? 'no detail'}`}
                    {command.status === 'expired' && 'expired — the host did not pick it up within 10 minutes'}
                  </p>
                  {done && command.result && <pre className={styles.result}>{JSON.stringify(command.result, null, 2)}</pre>}
                  {!finished && (
                    <p className={styles.muted}>The agent asks for commands every 15 seconds. You can close this; the command log keeps the result.</p>
                  )}
                  <div className={dialog.actions}>
                    <Dialog.Close className={shared.button}>Close</Dialog.Close>
                  </div>
                </div>
              )}
            </>
          )}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
