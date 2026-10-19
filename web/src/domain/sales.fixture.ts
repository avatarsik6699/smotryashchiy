import type { BusinessDocument, BusinessSummary } from './sales'

/** A sample summary in the site's schema, generated 2026-10-19 12:00 Moscow time. */
export function sampleSummary(overrides: Partial<BusinessSummary> = {}): BusinessSummary {
  return {
    schema: 'kodumka.business/1',
    generated_at: '2026-10-19T09:00:00+00:00',
    billing_enabled: true,
    paywall_enabled: false,
    funnel: [
      { day: '2026-10-19', plan: 'month', orders: 3, paid: 2, failed: 1, open: 0, refunded: 0, kopecks: 98_000 },
      { day: '2026-10-19', plan: 'year', orders: 1, paid: 1, failed: 0, open: 0, refunded: 0, kopecks: 299_000 },
      { day: '2026-10-15', plan: 'month', orders: 2, paid: 1, failed: 0, open: 1, refunded: 1, kopecks: 20_000 },
      { day: '2026-08-01', plan: 'month', orders: 5, paid: 5, failed: 0, open: 0, refunded: 0, kopecks: 245_000 },
    ],
    registrations: [
      { day: '2026-10-19', accounts: 4, confirmed: 3 },
      { day: '2026-10-14', accounts: 2, confirmed: 2 },
    ],
    accounts_total: 40,
    paying_accounts_total: 9,
    access: { active: 7, ending_7d: 2 },
    npd: { year: 2026, income_kopecks: 1_800_000_00, limit_kopecks: 2_400_000_00 },
    problems: { stuck_orders: 0, refunds_requested: 0, refunds_pending: 0, refunds_failed: 0 },
    reconcile: { checked_at: '2026-10-19T00:00:00+00:00', checked: 3, mismatches: 0 },
    ...overrides,
  }
}

export function sampleDocument(body: BusinessSummary = sampleSummary()): BusinessDocument {
  return { host_id: 'h1', host_name: 'infraege-prod', received_at: '2026-10-19T09:01:00Z', body }
}
