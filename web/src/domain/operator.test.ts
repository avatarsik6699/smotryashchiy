import { describe, expect, it } from 'vitest'
import { accountName, formatLeft, needsPassword, needsReason, rknDeadlines } from './operator'
import { sampleIncident } from './operator.fixture'

const hour = 3_600_000

describe('operator domain (Change 30)', () => {
  it('counts the RKN deadlines of a leak from detection', () => {
    const detected = new Date('2026-10-24T00:00:00Z').getTime()
    const [primary, final] = rknDeadlines(sampleIncident(), detected + 2 * hour)
    expect(primary).toMatchObject({ state: 'open', left: 22 * hour })
    expect(final).toMatchObject({ state: 'open', left: 70 * hour })
    expect(rknDeadlines(sampleIncident(), detected + 20 * hour)[0]!.state).toBe('soon')
    expect(rknDeadlines(sampleIncident(), detected + 25 * hour)[0]!.state).toBe('overdue')
    expect(rknDeadlines(sampleIncident({ rkn_primary_at: '2026-10-24T05:00:00Z' }), detected + 25 * hour)[0]!.state).toBe('done')
    expect(rknDeadlines(sampleIncident({ kind: 'outage' }), detected)).toEqual([])
  })

  it('formats time left and overdue time', () => {
    expect(formatLeft(3 * hour + 5 * 60_000)).toBe('3 h 05 min left')
    expect(formatLeft(-(2 * hour + 10 * 60_000))).toBe('overdue by 2 h 10 min')
    expect(formatLeft(30 * 60_000)).toBe('30 min left')
  })

  it('knows which commands need a reason and the password', () => {
    expect(['warn', 'suspend', 'terminate'].every((k) => needsReason(k as never))).toBe(true)
    expect(needsReason('lift')).toBe(false)
    expect(needsPassword('terminate') && needsPassword('refund_execute')).toBe(true)
    expect(needsPassword('warn')).toBe(false)
  })

  it('names an account by email, else by its id, else as deleted', () => {
    expect(accountName('a@b.c', 'x')).toBe('a@b.c')
    expect(accountName(null, '0b6f2c1e-5d4a')).toBe('account 0b6f2c1e')
    expect(accountName(null, null)).toBe('deleted account')
  })
})
