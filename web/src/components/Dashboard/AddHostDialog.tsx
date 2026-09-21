import { Button } from '@base-ui/react/button'
import { Dialog } from '@base-ui/react/dialog'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useRef, useState } from 'react'
import { api, ApiError } from '../../api/client'
import { useStore } from '../../data/DashboardContext'
import { formatClock } from '../../domain/format'
import shared from './Dashboard.module.css'
import styles from './AddHostDialog.module.css'

interface Created {
  server_url: string
  secret: string
  expires_at: string
  host: { name: string }
}

export function createErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.kind === 'network') return 'Cannot reach the server. Check the connection and try again.'
    if (error.status === 409) return 'A host with this name already exists.'
    if (error.status === 400) return `Invalid name: ${error.message}.`
    if (error.kind === 'rate_limited') return 'Too many requests. Try again shortly.'
    return `The server answered with an error (${error.status}). Try again.`
  }
  return 'Could not create the host. Try again.'
}

export function enrollCommand(created: Pick<Created, 'server_url' | 'secret'>): string {
  return `smotryashchiy agent enroll --server ${created.server_url} --secret ${created.secret}`
}

type CopyState = 'idle' | 'copied' | 'failed'

interface AddHostDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Creates a host and shows its one-time enrollment command. The secret lives only in this dialog's state. */
export function AddHostDialog({ open, onOpenChange }: AddHostDialogProps) {
  const store = useStore()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<Created | null>(null)
  const [copy, setCopy] = useState<CopyState>('idle')
  const nameRef = useRef<HTMLInputElement>(null)

  function handleOpenChange(next: boolean) {
    if (!next) {
      // Forget the secret as soon as the dialog closes.
      setCreated(null)
      setError(null)
      setCopy('idle')
      setPending(false)
    }
    onOpenChange(next)
  }

  async function submit(values: Record<string, unknown>) {
    const name = typeof values.name === 'string' ? values.name.trim() : ''
    setPending(true)
    setError(null)
    try {
      const result = await api<Created>('/api/hosts', { method: 'POST', body: { name } })
      setCreated(result)
      void store.load() // the new host appears in the ledger
    } catch (e) {
      setError(createErrorMessage(e))
    } finally {
      setPending(false)
    }
  }

  async function copyCommand() {
    if (!created) return
    try {
      await navigator.clipboard.writeText(enrollCommand(created))
      setCopy('copied')
    } catch {
      setCopy('failed')
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={styles.backdrop} />
        <Dialog.Popup className={styles.popup} initialFocus={nameRef}>
          <Dialog.Title className={styles.title}>Add host</Dialog.Title>
          {created === null ? (
            <>
              <Dialog.Description className={styles.description}>Name the host to get the one-time command that enrolls its agent.</Dialog.Description>
              <Form className={styles.form} onFormSubmit={submit}>
                <Field.Root name="name" className={styles.field}>
                  <Field.Label className={styles.label}>Host name</Field.Label>
                  <Field.Control ref={nameRef} required maxLength={80} autoComplete="off" spellCheck={false} className={styles.input} aria-invalid={error !== null} aria-describedby={error ? 'add-host-error' : undefined} />
                </Field.Root>
                <p id="add-host-error" role="alert" className={styles.error}>
                  {error}
                </p>
                <div className={styles.actions}>
                  <Dialog.Close className={shared.button}>Cancel</Dialog.Close>
                  <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.buttonPrimary}>
                    {pending ? 'Creating…' : 'Create host'}
                  </Button>
                </div>
              </Form>
            </>
          ) : (
            <>
              <Dialog.Description className={styles.description}>
                Run this on <strong>{created.host.name}</strong>. It is shown once and expires at {formatClock(Date.parse(created.expires_at))}.
              </Dialog.Description>
              <pre className={styles.command} tabIndex={0} aria-label="Enrollment command">
                <code>{enrollCommand(created)}</code>
              </pre>
              <div className={styles.actions}>
                <Button className={shared.button} onClick={() => void copyCommand()}>
                  Copy command
                </Button>
                <Dialog.Close className={shared.buttonPrimary}>Done</Dialog.Close>
              </div>
              <p className={styles.status} role="status">
                {copy === 'copied' && 'Copied to the clipboard.'}
                {copy === 'failed' && 'Could not copy. Select the command and copy it manually.'}
              </p>
            </>
          )}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
