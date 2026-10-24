import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import {
  FINISHED,
  type AuditEntry,
  type CommandKind,
  type Incident,
  type OperatorCommand,
  type OperatorDocument,
} from '../domain/operator'

export interface OperatorData {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  /** The operator reports, one per reporting host (normally one: kodumka); the view checks the schema. */
  documents: OperatorDocument[]
  commands: OperatorCommand[]
  incidents: Incident[]
  audit: AuditEntry[]
  reload: () => void
}

/** The report is written every ten minutes; a minute's refresh keeps an open tab current. */
const REFRESH_MS = 60_000

/** Loads the console's state when the view opens and refreshes it every minute (docs/SPEC.md §5). */
export function useOperator(): OperatorData {
  const [state, setState] = useState<Omit<OperatorData, 'reload'>>({
    status: 'loading',
    error: null,
    documents: [],
    commands: [],
    incidents: [],
    audit: [],
  })
  const [key, setKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    Promise.all([
      api<{ documents: OperatorDocument[] }>('/api/documents?kind=operator'),
      api<{ commands: OperatorCommand[] }>('/api/operator/commands?limit=50'),
      api<{ incidents: Incident[] }>('/api/operator/incidents'),
      api<{ entries: AuditEntry[] }>('/api/audit?limit=100'),
    ]).then(
      ([docs, commands, incidents, audit]) => {
        if (cancelled) return
        setState({
          status: 'ready',
          error: null,
          documents: docs.documents,
          commands: commands.commands,
          incidents: incidents.incidents,
          audit: audit.entries,
        })
      },
      (e: unknown) => {
        if (!cancelled)
          setState((s) => ({ ...s, status: s.status === 'ready' ? 'ready' : 'error', error: e instanceof Error ? e.message : 'load failed' }))
      },
    )
    const timer = window.setTimeout(() => setKey((k) => k + 1), REFRESH_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [key])

  const reload = useCallback(() => setKey((k) => k + 1), [])
  return { ...state, reload }
}

export interface QueueRequest {
  host_id: string
  kind: CommandKind
  args: Record<string, unknown>
  reason?: string
  password?: string
}

/** How long the UI waits for the host's answer before showing the command as still running. */
const WAIT_MS = 90_000
const POLL_MS = 1_500

/** Queues one command and polls it until the host answered or the wait is over. */
export function useCommand() {
  const [command, setCommand] = useState<OperatorCommand | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [pending, setPending] = useState(false)
  const stopped = useRef(false)

  useEffect(
    () => () => {
      stopped.current = true
    },
    [],
  )

  const run = useCallback(async (req: QueueRequest): Promise<OperatorCommand | null> => {
    setPending(true)
    setError(null)
    setCommand(null)
    try {
      let { command: current } = await api<{ command: OperatorCommand }>('/api/operator/commands', { method: 'POST', body: req })
      setCommand(current)
      const deadline = Date.now() + WAIT_MS
      while (!FINISHED.includes(current.status) && Date.now() < deadline && !stopped.current) {
        await new Promise((resolve) => window.setTimeout(resolve, POLL_MS))
        current = (await api<{ command: OperatorCommand }>(`/api/operator/commands/${current.id}`)).command
        setCommand(current)
      }
      return current
    } catch (e) {
      setError(e instanceof Error ? e : new Error('request failed'))
      return null
    } finally {
      setPending(false)
    }
  }, [])

  const reset = useCallback(() => {
    setCommand(null)
    setError(null)
    setPending(false)
  }, [])

  return { command, error, pending, run, reset }
}
