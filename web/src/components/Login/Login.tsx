import { Button } from '@base-ui/react/button'
import { Field } from '@base-ui/react/field'
import { Form } from '@base-ui/react/form'
import { useState } from 'react'
import { ApiError } from '../../api/client'
import styles from './Login.module.css'

interface LoginProps {
  onLogin: (password: string) => Promise<void>
}

/** Text for each way a login attempt can fail; each is distinct so the operator knows what to do. */
export function loginErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.kind) {
      case 'unauthorized':
        return 'Wrong password.'
      case 'rate_limited':
        return error.retryAfter === undefined
          ? 'Too many attempts. Try again later.'
          : `Too many attempts. Try again in ${error.retryAfter} s.`
      case 'network':
        return 'Cannot reach the server. Check the connection and try again.'
      case 'http':
        return `The server answered with an error (${error.status}). Try again.`
    }
  }
  return 'Login failed. Try again.'
}

export function Login({ onLogin }: LoginProps) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(values: Record<string, unknown>) {
    const password = typeof values.password === 'string' ? values.password : ''
    setPending(true)
    setError(null)
    try {
      await onLogin(password)
    } catch (e) {
      setError(loginErrorMessage(e))
      setPending(false)
    }
  }

  return (
    <main className={styles.page}>
      <Form className={styles.form} onFormSubmit={submit} aria-labelledby="login-title">
        <h1 id="login-title" className={styles.title}>
          <span className={styles.prompt} aria-hidden="true">
            ${' '}
          </span>
          smotryashchiy
        </h1>
        {/* No `invalid` prop on Field.Root: Base UI blocks form submission while a field is invalid, which
            would make a wrong password impossible to retry. The error is conveyed by aria-invalid + text. */}
        {/* Password managers and Chrome expect a username next to a password field; the single admin
            has none, so this stays visually hidden and out of the accessibility tree. */}
        <input type="text" name="username" autoComplete="username" tabIndex={-1} aria-hidden="true" className="visually-hidden" />
        <Field.Root name="password" className={styles.field}>
          <Field.Label className={styles.label}>Password</Field.Label>
          <Field.Control
            type="password"
            required
            autoFocus
            autoComplete="current-password"
            className={styles.input}
            aria-invalid={error !== null}
            aria-describedby={error ? 'login-error' : undefined}
          />
        </Field.Root>
        <p id="login-error" role="alert" className={styles.error}>
          {error}
        </p>
        <Button type="submit" disabled={pending} focusableWhenDisabled className={styles.button}>
          {pending ? 'Signing in…' : 'Sign in'}
        </Button>
      </Form>
    </main>
  )
}
