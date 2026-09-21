import { useCallback, useEffect, useState } from 'react'
import { api, onUnauthorized } from '../api/client'

/**
 * checking: probing the session; anonymous: show the login form; authenticated: show the app;
 * unreachable: the server could not be asked (distinct from "logged out").
 */
export type SessionState = 'checking' | 'anonymous' | 'authenticated' | 'unreachable'

export interface Session {
  state: SessionState
  login: (password: string) => Promise<void>
  logout: () => Promise<void>
  retry: () => void
}

export function useSession(): Session {
  const [state, setState] = useState<SessionState>('checking')
  const [probe, setProbe] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setState('checking')
    // A dedicated endpoint that answers 200 either way: a logged-out load must not leave a failed
    // request (a red 401) in the browser console.
    api<{ authenticated: boolean }>('/api/auth/session', { signal: controller.signal }).then(
      (answer) => setState(answer.authenticated ? 'authenticated' : 'anonymous'),
      () => {
        if (!controller.signal.aborted) setState('unreachable')
      },
    )
    return () => controller.abort()
  }, [probe])

  useEffect(() => onUnauthorized(() => setState('anonymous')), [])

  const login = useCallback(async (password: string) => {
    await api('/api/auth/login', { method: 'POST', body: { password }, expectUnauthorized: true })
    setState('authenticated')
  }, [])

  const logout = useCallback(async () => {
    try {
      await api('/api/auth/logout', { method: 'POST' })
    } catch {
      // Whatever the answer, this browser no longer treats itself as logged in.
    }
    setState('anonymous')
  }, [])

  const retry = useCallback(() => setProbe((n) => n + 1), [])

  return { state, login, logout, retry }
}
