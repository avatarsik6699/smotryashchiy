import { describe, expect, it } from 'vitest'
import { bucketLabel, delta, displayValue, formatCount, formatDuration, intervalsFor, metricLabel, rangeDays } from './analytics'

describe('delta', () => {
  it('colours by better or worse and points by sign', () => {
    expect(delta('visitors', 118, 100)).toEqual({ text: '+18%', direction: 'up', better: true })
    expect(delta('visit_duration', 92, 100)).toEqual({ text: '−8%', direction: 'down', better: false })
    // lower is better for the bounce rate, in percentage points
    expect(delta('bounce_rate', 46, 49)).toEqual({ text: '−3 pp', direction: 'down', better: true })
    expect(delta('bounce_rate', 52, 49)).toEqual({ text: '+3 pp', direction: 'up', better: false })
    expect(delta('views_per_visit', 2.9, 2.7)).toEqual({ text: '+0.2', direction: 'up', better: true })
  })

  it('is flat without a change and absent without an earlier value', () => {
    expect(delta('visits', 100, 100)).toEqual({ text: '0%', direction: 'flat', better: null })
    expect(delta('visits', 5, 0)).toBeNull()
    expect(delta('visits', 5, undefined)).toBeNull()
  })
})

describe('formats', () => {
  it('groups counts and spells durations', () => {
    expect(formatCount(12480)).toBe('12 480')
    expect(formatCount(999)).toBe('999')
    expect(formatDuration(230)).toBe('3m 50s')
    expect(formatDuration(42)).toBe('42s')
    expect(formatDuration(3720)).toBe('1h 2m')
  })

  it('names visitor-days over several days', () => {
    expect(metricLabel('visitors', 1)).toBe('Visitors')
    expect(metricLabel('visitors', 30)).toBe('Visitor-days')
    expect(rangeDays(['2026-09-27', '2026-10-26'])).toBe(30)
  })

  it('labels buckets in the site clock', () => {
    expect(bucketLabel('hour', '2026-10-26T14:00:00+03:00')).toBe('14:00')
    expect(bucketLabel('day', '2026-10-16', true)).toBe('Fri 16 Oct')
    expect(bucketLabel('week', '2026-10-12', true)).toBe('week of Mon 12 Oct')
    expect(bucketLabel('month', '2026-10-01')).toBe('Oct 2026')
  })

  it('reads stored values', () => {
    expect(displayValue('channel', 'organic_search')).toBe('Organic search')
    expect(displayValue('country', 'RU')).toBe('Russia')
    expect(displayValue('country', '')).toBe('(unknown)')
    expect(displayValue('source', '')).toBe('Direct / none')
    expect(displayValue('utm_campaign', '')).toBe('(none)')
    expect(displayValue('browser_version', '131', [{ dimension: 'browser', op: 'is', values: ['Chrome'] }])).toBe('Chrome 131')
  })

  it('offers hours for one day and months for a year', () => {
    expect(intervalsFor('today')).toEqual(['hour', 'day'])
    expect(intervalsFor('30d')).toEqual(['day', 'week'])
    expect(intervalsFor('12mo')[0]).toBe('month')
  })
})
