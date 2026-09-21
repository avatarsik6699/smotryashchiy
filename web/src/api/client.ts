// Typed fetch wrapper for the JSON API. Every failure becomes an ApiError with a `kind`, so screens
// can show distinct states (wrong password, rate limited, unreachable) instead of one generic error.

export type ApiErrorKind = 'unauthorized' | 'rate_limited' | 'network' | 'http'

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly status: number
  /** Seconds the server asked us to wait (429 only). */
  readonly retryAfter: number | undefined

  constructor(kind: ApiErrorKind, status: number, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.kind = kind
    this.status = status
    this.retryAfter = retryAfter
  }
}

const unauthorizedTarget = new EventTarget()
const UNAUTHORIZED = 'unauthorized'

/** Calls listener whenever an authenticated call was refused with 401 (session expired or missing). */
export function onUnauthorized(listener: () => void): () => void {
  unauthorizedTarget.addEventListener(UNAUTHORIZED, listener)
  return () => unauthorizedTarget.removeEventListener(UNAUTHORIZED, listener)
}

/** Announces that the session is gone (an authenticated call was refused, or a probe found no session). */
export function signalUnauthorized(): void {
  unauthorizedTarget.dispatchEvent(new Event(UNAUTHORIZED))
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'DELETE'
  body?: unknown
  /** A 401 from this call is an answer (wrong password), not an expired session. */
  expectUnauthorized?: boolean
  signal?: AbortSignal
}

export function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined
  const seconds = Number(header)
  return Number.isFinite(seconds) && seconds >= 0 ? Math.ceil(seconds) : undefined
}

async function errorMessage(res: Response): Promise<string> {
  try {
    const body: unknown = await res.json()
    if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
      return body.error
    }
  } catch {
    // not JSON: fall through to the status text
  }
  return res.statusText || `HTTP ${res.status}`
}

export async function api<T = void>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, expectUnauthorized = false, signal } = options
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    })
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError('network', 0, 'cannot reach the server')
  }

  if (res.ok) {
    return (res.status === 204 ? undefined : await res.json()) as T
  }
  const message = await errorMessage(res)
  if (res.status === 401) {
    if (!expectUnauthorized) signalUnauthorized()
    throw new ApiError('unauthorized', 401, message)
  }
  if (res.status === 429) {
    throw new ApiError('rate_limited', 429, message, parseRetryAfter(res.headers.get('Retry-After')))
  }
  throw new ApiError('http', res.status, message)
}
