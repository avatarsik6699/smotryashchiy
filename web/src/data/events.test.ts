import { describe, expect, it } from 'vitest'
import type { EventDTO } from '../domain/types'
import { eventSource, eventSources } from './events'

const event = (labels: Record<string, string>): EventDTO => ({
  host: 'a',
  ts: '2026-09-22T12:00:00Z',
  level: 'info',
  message: 'x',
  labels,
})

describe('eventSource', () => {
  it('prefers unit, then container, then jail', () => {
    expect(eventSource({ unit: 'ssh.service', container: 'web', jail: 'sshd' })).toBe('ssh.service')
    expect(eventSource({ container: 'web', jail: 'sshd' })).toBe('web')
    expect(eventSource({ jail: 'sshd' })).toBe('sshd')
    expect(eventSource({})).toBeNull()
  })
})

describe('eventSources', () => {
  it('returns the distinct sorted sources, ignoring unlabeled events', () => {
    const events = [event({ unit: 'b.service' }), event({ jail: 'sshd' }), event({ unit: 'b.service' }), event({})]
    expect(eventSources(events)).toEqual(['b.service', 'sshd'])
  })
})
