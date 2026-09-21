import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, onUnauthorized, parseRetryAfter } from './client'

function respond(status: number, body?: unknown, headers: Record<string, string> = {}): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), { status, headers })
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

describe('api', () => {
  it('returns parsed JSON and sends same-origin credentials', async () => {
    fetchMock.mockResolvedValue(respond(200, { hosts: [] }))
    await expect(api<{ hosts: unknown[] }>('/api/hosts')).resolves.toEqual({ hosts: [] })
    expect(fetchMock).toHaveBeenCalledWith('/api/hosts', expect.objectContaining({ credentials: 'same-origin', method: 'GET' }))
  })

  it('sends JSON bodies with a content type and treats 204 as no content', async () => {
    fetchMock.mockResolvedValue(respond(204))
    await expect(api('/api/auth/login', { method: 'POST', body: { password: 'x' } })).resolves.toBeUndefined()
    const init = fetchMock.mock.calls[0]?.[1]
    expect(init?.body).toBe('{"password":"x"}')
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' })
  })

  it('signals an expired session on 401 and still throws', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'invalid or expired session' }))
    const listener = vi.fn()
    const off = onUnauthorized(listener)
    await expect(api('/api/hosts')).rejects.toMatchObject({ kind: 'unauthorized', status: 401, message: 'invalid or expired session' })
    expect(listener).toHaveBeenCalledTimes(1)
    off()
  })

  it('does not signal an expired session when 401 is the expected answer (wrong password)', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'invalid credentials' }))
    const listener = vi.fn()
    const off = onUnauthorized(listener)
    await expect(api('/api/auth/login', { method: 'POST', body: {}, expectUnauthorized: true })).rejects.toMatchObject({ kind: 'unauthorized' })
    expect(listener).not.toHaveBeenCalled()
    off()
  })

  it('reads Retry-After on 429', async () => {
    fetchMock.mockResolvedValue(respond(429, { error: 'too many login attempts' }, { 'Retry-After': '42' }))
    await expect(api('/api/auth/login', { method: 'POST' })).rejects.toMatchObject({ kind: 'rate_limited', retryAfter: 42 })
  })

  it('maps a network failure to a distinct kind', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    await expect(api('/api/hosts')).rejects.toMatchObject({ kind: 'network', status: 0 })
  })

  it('lets aborts through untouched so callers can ignore them', async () => {
    fetchMock.mockRejectedValue(new DOMException('aborted', 'AbortError'))
    await expect(api('/api/hosts')).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('uses the server error text, falling back to the status text for non-JSON bodies', async () => {
    fetchMock.mockResolvedValueOnce(respond(409, { error: 'host name already exists' }))
    await expect(api('/api/hosts', { method: 'POST' })).rejects.toMatchObject({ kind: 'http', status: 409, message: 'host name already exists' })
    fetchMock.mockResolvedValueOnce(new Response('<html>bad gateway</html>', { status: 502, statusText: 'Bad Gateway' }))
    const error = await api('/api/hosts').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ kind: 'http', status: 502, message: 'Bad Gateway' })
  })
})

describe('parseRetryAfter', () => {
  it('accepts non-negative numbers and rounds up', () => {
    expect(parseRetryAfter('3')).toBe(3)
    expect(parseRetryAfter('2.2')).toBe(3)
    expect(parseRetryAfter('0')).toBe(0)
  })
  it('ignores missing and invalid values', () => {
    expect(parseRetryAfter(null)).toBeUndefined()
    expect(parseRetryAfter('soon')).toBeUndefined()
    expect(parseRetryAfter('-1')).toBeUndefined()
  })
})
