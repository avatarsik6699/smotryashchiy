import { describe, expect, it } from 'vitest'
import { formatLatency, UNKNOWN } from './format'
import { tlsDaysLeft, tlsLabel, uptimeState } from './uptime'

const NOW = Date.UTC(2026, 8, 21, 12, 0, 0)
const at = (offsetSeconds: number, ok = true) => ({ ts: new Date(NOW + offsetSeconds * 1000).toISOString(), ok })

describe('uptimeState', () => {
  it('is NEW without a result', () => {
    expect(uptimeState(null, 60, NOW)).toBe('new')
  })
  it('follows the last result while it is recent', () => {
    expect(uptimeState(at(-30, true), 60, NOW)).toBe('up')
    expect(uptimeState(at(-30, false), 60, NOW)).toBe('down')
  })
  it('goes STALE after max(3 x interval, 90 s): a silent prober says nothing about the target', () => {
    expect(uptimeState(at(-179), 60, NOW)).toBe('up')
    expect(uptimeState(at(-181), 60, NOW)).toBe('stale')
    expect(uptimeState(at(-89), 30, NOW)).toBe('up') // 3 x 30 s = 90 s floor
    expect(uptimeState(at(-91), 30, NOW)).toBe('stale')
    expect(uptimeState(at(-3000, false), 3600, NOW)).toBe('down') // an hourly probe is not stale after 50 minutes
  })
  it('tolerates a result slightly in the future (clock skew)', () => {
    expect(uptimeState(at(5), 60, NOW)).toBe('up')
  })
})

describe('tls', () => {
  const cert = (days: number) => ({ cert_expires_at: new Date(NOW + days * 86_400_000 + 3600_000).toISOString() })
  it('counts whole days left and labels them', () => {
    expect(tlsDaysLeft(cert(9), NOW)).toBe(9)
    expect(tlsLabel(9)).toBe('TLS 9d')
    expect(tlsLabel(0)).toBe('TLS 0d')
  })
  it('reports an expired certificate in words', () => {
    const days = tlsDaysLeft({ cert_expires_at: new Date(NOW - 2 * 86_400_000).toISOString() }, NOW)
    expect(days).toBeLessThan(0)
    expect(tlsLabel(days)).toBe('TLS expired')
  })
  it('is unknown without certificate information', () => {
    expect(tlsDaysLeft(null, NOW)).toBeNull()
    expect(tlsDaysLeft({ cert_expires_at: null }, NOW)).toBeNull()
    expect(tlsLabel(null)).toBeNull()
  })
})

describe('formatLatency', () => {
  it('uses ms below a second and seconds above', () => {
    expect(formatLatency(0)).toBe('0 ms') // a measured zero is still a value
    expect(formatLatency(142.4)).toBe('142 ms')
    expect(formatLatency(1300)).toBe('1.3 s')
    expect(formatLatency(-1)).toBe(UNKNOWN)
    expect(formatLatency(NaN)).toBe(UNKNOWN)
  })
})
