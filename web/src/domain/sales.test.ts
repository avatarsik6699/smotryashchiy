import { describe, expect, it } from 'vitest'
import { dayRows, formatRubles, moscowDay, npdLevel, planNames, problems, rangeDays, totals } from './sales'
import { sampleSummary } from './sales.fixture'

describe('sales summary (Change 27)', () => {
  it('counts days in Moscow time, newest first, keeping empty days', () => {
    expect(moscowDay('2026-10-18T22:30:00Z')).toBe('2026-10-19')
    expect(rangeDays('2026-10-19T09:00:00Z', 3)).toEqual(['2026-10-19', '2026-10-18', '2026-10-17'])
    const rows = dayRows(sampleSummary(), 7)
    expect(rows).toHaveLength(7)
    expect(rows[0]).toMatchObject({ day: '2026-10-19', orders: 4, paid: 3, kopecks: 397_000, accounts: 4 })
    expect(rows[0]?.plans).toEqual({ month: { orders: 3, paid: 2 }, year: { orders: 1, paid: 1 } })
    expect(rows[1]).toMatchObject({ day: '2026-10-18', orders: 0, accounts: 0 })
  })

  it('totals a range and leaves older days out', () => {
    const week = totals(dayRows(sampleSummary(), 7))
    expect(week).toMatchObject({ orders: 6, paid: 4, refunded: 1, kopecks: 417_000, accounts: 6 })
    expect(week.conversion).toBeCloseTo(4 / 6)
    expect(totals(dayRows(sampleSummary({ funnel: [] }), 7)).conversion).toBeNull()
  })

  it('orders plans month, year, then others', () => {
    const s = sampleSummary()
    s.funnel.push({ ...s.funnel[0]!, plan: 'family' })
    expect(planNames(s)).toEqual(['month', 'year', 'family'])
  })

  it('names problems by severity and stays quiet without them', () => {
    expect(problems(sampleSummary())).toEqual([])
    const p = problems(
      sampleSummary({
        problems: { stuck_orders: 2, refunds_requested: 0, refunds_pending: 1, refunds_failed: 1 },
        reconcile: { checked_at: '2026-10-19T00:00:00Z', checked: 3, mismatches: 1 },
      }),
    )
    expect(p.map((x) => x.level)).toEqual(['critical', 'critical', 'warn', 'warn'])
    expect(p.map((x) => x.text)).toEqual([
      '1 refund failed',
      '1 payment differs from the provider',
      '2 orders stuck over 2 h',
      '1 refund pending at the provider',
    ])
  })

  it('reads the НПД share as a state only near the limit', () => {
    expect([npdLevel(0.5), npdLevel(0.7), npdLevel(0.85)]).toEqual(['ok', 'warn', 'critical'])
    expect(formatRubles(1_234_567_00)).toBe('1 234 567 ₽')
  })
})
