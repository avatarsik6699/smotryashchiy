import { Button } from '@base-ui/react/button'
import { Dialog } from '@base-ui/react/dialog'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useRef, useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { SiteDTO } from '../../domain/types'
import shared from '../Dashboard/Dashboard.module.css'
import styles from './AddSiteDialog.module.css'

export function createErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.kind === 'network') return 'Cannot reach the server. Check the connection and try again.'
    if (error.status === 409) return 'A site with this domain already exists.'
    if (error.status === 400) return `Invalid input: ${error.message}.`
    if (error.kind === 'rate_limited') return 'Too many requests. Try again shortly.'
    return `The server answered with an error (${error.status}). Try again.`
  }
  return 'Could not create the site. Try again.'
}

/** The `<script>` tag to paste into the tracked site (docs/SPEC.md §4i, §5). */
export function trackingSnippet(site: Pick<SiteDTO, 'id'>): string {
  return `<script defer src="${location.origin}/track.js" data-site="${site.id}"></script>`
}

type CopyState = 'idle' | 'copied' | 'failed'

interface AddSiteDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

/** Creates a site and shows its tracking snippet. Mirrors AddHostDialog's shape. */
export function AddSiteDialog({ open, onOpenChange, onCreated }: AddSiteDialogProps) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<SiteDTO | null>(null)
  const [copy, setCopy] = useState<CopyState>('idle')
  const nameRef = useRef<HTMLInputElement>(null)

  function handleOpenChange(next: boolean) {
    if (!next) {
      setCreated(null)
      setError(null)
      setCopy('idle')
      setPending(false)
    }
    onOpenChange(next)
  }

  async function submit(values: Record<string, unknown>) {
    const name = typeof values.name === 'string' ? values.name.trim() : ''
    const domain = typeof values.domain === 'string' ? values.domain.trim() : ''
    setPending(true)
    setError(null)
    try {
      const site = await api<SiteDTO>('/api/sites', {
        method: 'POST',
        body: { name, domain },
      })
      setCreated(site)
      onCreated()
    } catch (e) {
      setError(createErrorMessage(e))
    } finally {
      setPending(false)
    }
  }

  async function copySnippet() {
    if (!created) return
    try {
      await navigator.clipboard.writeText(trackingSnippet(created))
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
          <Dialog.Title className={styles.title}>Add site</Dialog.Title>
          {created === null ? (
            <>
              <Dialog.Description className={styles.description}>Name the site and give its domain to get a tracking snippet.</Dialog.Description>
              <Form className={styles.form} onFormSubmit={submit}>
                <Field.Root name="name" className={styles.field}>
                  <Field.Label className={styles.label}>Site name</Field.Label>
                  <Field.Control ref={nameRef} required maxLength={80} autoComplete="off" spellCheck={false} className={styles.input} />
                </Field.Root>
                <Field.Root name="domain" className={styles.field}>
                  <Field.Label className={styles.label}>Domain</Field.Label>
                  <Field.Control
                    required
                    maxLength={253}
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="example.com"
                    className={styles.input}
                    aria-invalid={error !== null}
                    aria-describedby={error ? 'add-site-error' : undefined}
                  />
                </Field.Root>
                <p id="add-site-error" role="alert" className={styles.error}>
                  {error}
                </p>
                <div className={styles.actions}>
                  <Dialog.Close className={shared.button}>Cancel</Dialog.Close>
                  <Button type="submit" disabled={pending} focusableWhenDisabled className={shared.buttonPrimary}>
                    {pending ? 'Creating…' : 'Create site'}
                  </Button>
                </div>
              </Form>
            </>
          ) : (
            <>
              <Dialog.Description className={styles.description}>
                Paste this on <strong>{created.domain}</strong>, anywhere in <code>&lt;head&gt;</code> or <code>&lt;body&gt;</code>.
              </Dialog.Description>
              <pre className={styles.command} tabIndex={0} aria-label="Tracking snippet">
                <code>{trackingSnippet(created)}</code>
              </pre>
              <p className={styles.notice}>
                If <strong>{created.domain}</strong> sets its own Content-Security-Policy, add this server's origin (<code>{location.origin}</code>) to both <code>script-src</code> and{' '}
                <code>connect-src</code> — otherwise the snippet silently fails to load or send data, with no error visible here.
              </p>
              <div className={styles.actions}>
                <Button className={shared.button} onClick={() => void copySnippet()}>
                  Copy snippet
                </Button>
                <Dialog.Close className={shared.buttonPrimary}>Done</Dialog.Close>
              </div>
              <p className={styles.status} role="status">
                {copy === 'copied' && 'Copied to the clipboard.'}
                {copy === 'failed' && 'Could not copy. Select the snippet and copy it manually.'}
              </p>
            </>
          )}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
