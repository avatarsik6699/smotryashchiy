import { Button } from '@base-ui/react/button'
import { Dialog } from '@base-ui/react/dialog'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { Radio } from '@base-ui/react/radio'
import { RadioGroup } from '@base-ui/react/radio-group'
import { useId, useRef, useState } from 'react'
import { api, ApiError } from '../../api/client'
import { useStore } from '../../data/DashboardContext'
import type { UptimeKind } from '../../domain/types'
import dialog from './AddHostDialog.module.css'
import shared from './Dashboard.module.css'
import styles from './AddTargetDialog.module.css'

const KINDS: { value: UptimeKind; label: string; placeholder: string; hint: string }[] = [
  { value: 'http', label: 'HTTP', placeholder: 'https://example.com/health', hint: 'Up when the page answers 200–399 (redirects followed).' },
  { value: 'tcp', label: 'TCP', placeholder: 'db.internal:5432', hint: 'Up when the port accepts a connection.' },
  { value: 'tls', label: 'TLS', placeholder: 'example.com:443', hint: 'Up when the handshake succeeds; shows days left on the certificate.' },
]

export function targetErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.kind === 'network') return 'Cannot reach the server. Check the connection and try again.'
    if (error.status === 400 || error.status === 409) return `${error.message.charAt(0).toUpperCase()}${error.message.slice(1)}.`
    if (error.kind === 'rate_limited') return 'Too many requests. Try again shortly.'
    return `The server answered with an error (${error.status}). Try again.`
  }
  return 'Could not add the target. Try again.'
}

interface AddTargetDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Adds an uptime target; the ledger picks it up through a reload and the first probe arrives on the stream. */
export function AddTargetDialog({ open, onOpenChange }: AddTargetDialogProps) {
  const store = useStore()
  const kindLabelId = useId()
  const nameRef = useRef<HTMLInputElement>(null)
  const [kind, setKind] = useState<UptimeKind>('http')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const current = KINDS.find((k) => k.value === kind)!

  function handleOpenChange(next: boolean) {
    if (!next) {
      setKind('http')
      setError(null)
      setPending(false)
    }
    onOpenChange(next)
  }

  async function submit(values: Record<string, unknown>) {
    const text = (key: string) => (typeof values[key] === 'string' ? (values[key] as string).trim() : '')
    const interval = Number(text('interval'))
    setPending(true)
    setError(null)
    try {
      await api('/api/uptime', { method: 'POST', body: { name: text('name'), kind, target: text('target'), interval_seconds: Number.isFinite(interval) && interval > 0 ? interval : 0 } })
      await store.load()
      handleOpenChange(false)
    } catch (e) {
      setError(targetErrorMessage(e))
      setPending(false)
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={dialog.backdrop} />
        <Dialog.Popup className={dialog.popup} initialFocus={nameRef}>
          <Dialog.Title className={dialog.title}>Add uptime target</Dialog.Title>
          <Dialog.Description className={dialog.description}>The server probes it on the interval you choose and keeps the results.</Dialog.Description>
          <Form className={dialog.form} onFormSubmit={submit}>
            <Field.Root name="name" className={dialog.field}>
              <Field.Label className={dialog.label}>Name</Field.Label>
              <Field.Control ref={nameRef} required maxLength={80} autoComplete="off" spellCheck={false} className={dialog.input} />
            </Field.Root>

            <div className={dialog.field}>
              <span id={kindLabelId} className={dialog.label}>
                Type
              </span>
              <RadioGroup aria-labelledby={kindLabelId} value={kind} onValueChange={(v) => setKind(v as UptimeKind)} className={styles.kinds}>
                {KINDS.map((k) => (
                  <label key={k.value} className={styles.kind}>
                    <Radio.Root value={k.value} className={styles.radio}>
                      <Radio.Indicator className={styles.indicator} />
                    </Radio.Root>
                    {k.label}
                  </label>
                ))}
              </RadioGroup>
            </div>

            <Field.Root name="target" className={dialog.field}>
              <Field.Label className={dialog.label}>{kind === 'http' ? 'URL' : 'Host and port'}</Field.Label>
              <Field.Control required key={kind} placeholder={current.placeholder} autoComplete="off" spellCheck={false} className={dialog.input} aria-invalid={error !== null} aria-describedby="target-hint target-error" />
              <span id="target-hint" className={styles.hint}>
                {current.hint}
              </span>
            </Field.Root>

            <Field.Root name="interval" className={dialog.field}>
              <Field.Label className={dialog.label}>Interval, seconds</Field.Label>
              <Field.Control type="number" inputMode="numeric" min={30} max={3600} defaultValue={60} className={dialog.input} />
            </Field.Root>

            <p id="target-error" role="alert" className={dialog.error}>
              {error}
            </p>
            <div className={dialog.actions}>
              <Dialog.Close className={shared.button}>Cancel</Dialog.Close>
              <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.buttonPrimary}>
                {pending ? 'Adding…' : 'Add target'}
              </Button>
            </div>
          </Form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
