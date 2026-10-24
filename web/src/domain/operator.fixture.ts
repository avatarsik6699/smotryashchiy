import type { Incident, OperatorDocument, OperatorReport } from './operator'

/** A sample operator report in the site's schema, generated 2026-10-24 12:00 UTC. */
export function sampleReport(overrides: Partial<OperatorReport> = {}): OperatorReport {
  return {
    schema: 'kodumka.operator/1',
    generated_at: '2026-10-24T12:00:00+00:00',
    signal_days: 7,
    signals: [
      {
        user_id: '0b6f2c1e-5d4a-4c3b-9a8f-7e6d5c4b3a21',
        email: 'shared@example.com',
        flags: ['cities', 'evictions'],
        counters: { max_cities_per_day: 3, evictions: 4, new_devices: 2, max_paid_chunks_per_day: 30, max_solutions_per_day: 5 },
        sessions: [
          { id: 's1', label: 'Chrome · Windows', city: 'Москва', ip: '198.51.100.7', last_seen_at: '2026-10-24T11:50:00+00:00' },
          { id: 's2', label: 'Safari · iPhone', city: 'Казань', ip: '203.0.113.9', last_seen_at: '2026-10-24T11:40:00+00:00' },
        ],
        restriction: null,
      },
    ],
    restrictions: [
      {
        id: 'r1',
        kind: 'suspension',
        reason: 'доступ передан другим людям',
        created_at: '2026-10-22T10:00:00+00:00',
        ends_at: '2026-10-29T10:00:00+00:00',
        lifted_at: null,
        user_id: '1b6f2c1e-5d4a-4c3b-9a8f-7e6d5c4b3a21',
        email: 'other@example.com',
      },
    ],
    refunds: [
      {
        id: '2b6f2c1e-5d4a-4c3b-9a8f-7e6d5c4b3a21',
        invoice: 1004,
        user_id: null,
        email: null,
        amount_kopecks: 32_667,
        reason: 'account_deleted',
        status: 'requested',
        created_at: '2026-10-23T09:00:00+00:00',
      },
    ],
    ...overrides,
  }
}

export function sampleOperatorDocument(body: OperatorReport = sampleReport()): OperatorDocument {
  return { host_id: 'host-1', host_name: 'kodumka-prod', received_at: body.generated_at, body }
}

export function sampleIncident(overrides: Partial<Incident> = {}): Incident {
  return {
    id: 'i1',
    title: 'Утечка выгрузки',
    kind: 'leak',
    detected_at: '2026-10-24T00:00:00+00:00',
    scope: 'emails',
    people_affected: 12,
    measures: '',
    rkn_primary_at: null,
    rkn_final_at: null,
    users_notified_at: null,
    notes: '',
    closed_at: null,
    created_at: '2026-10-24T00:10:00+00:00',
    updated_at: '2026-10-24T00:10:00+00:00',
    ...overrides,
  }
}
