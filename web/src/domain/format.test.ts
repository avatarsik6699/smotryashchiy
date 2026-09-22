import { describe, expect, it } from 'vitest'
import { formatMeta } from './format'

describe('formatMeta', () => {
  it('renders a non-empty object as key=value pairs', () => {
    expect(formatMeta({ currently_banned: 5, jail: 'sshd' })).toBe('currently_banned=5, jail=sshd')
  })

  it('is empty for an empty object, null, or a non-object', () => {
    expect(formatMeta({})).toBe('')
    expect(formatMeta(null)).toBe('')
    expect(formatMeta('x')).toBe('')
    expect(formatMeta([1, 2])).toBe('')
  })
})
