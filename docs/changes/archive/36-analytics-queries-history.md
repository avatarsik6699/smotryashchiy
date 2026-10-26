# CHANGE 36 — Analytics queries and history: one query API, daily aggregates, comparison

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `36` |
| Slug | `analytics-queries-history` |
| Title | Analytics queries and history: one query API, daily aggregates, comparison |
| Status | `archived` |
| Branch | `feature/36-analytics-queries-history` |

---

## Goal

Stage S3 of the 2026-10-14 plan, the second of three changes (35 collection, 37 dashboard). It
gives the dashboard everything it reads:
- one strict query endpoint for totals, breakdowns and time series, with click filters and comparison
  to the previous period or the same period a year ago;
- the history: daily aggregates kept for 25 months, so a season compares with the last one;
- CSV export and the current-visitors count.

Architect decisions of 2026-10-26: raw analytics 90 days, aggregates 25 months, filters and
comparison (filter state is not in the URL — a Change 37 concern).

---

## Backlog

### Backend
- [x] `B1` Table `analytics_daily (site_id, day, dimension, value, visitors, visits, pageviews,
  bounces, duration_ms)`. `day` is the site's local date. Dimensions:
  - the total;
  - page, entry page, exit page;
  - channel, source, the five UTM, country, region, browser, browser version, OS, OS version, device,
    screen, hostname. — _Depends on:_ —
- [x] `B2` Aggregation job:
  - hourly, rebuilds today and yesterday of every site in its timezone;
  - backfills any day that has raw data and no aggregate yet;
  - drops aggregate days older than 25 months;
  - idempotent: a rerun replaces, never adds. — _Depends on:_ B1
- [x] `B3` `POST /api/sites/{id}/query` (session):
  - `metrics` ⊆ {visitors, visits, pageviews, views_per_visit, bounce_rate, visit_duration};
  - at most one `dimension`: one of B1's or `time:hour|day|week|month`;
  - `filters` [{dimension, op is|is_not|contains, values}], combined with AND;
  - `date_range` [from, to] in the site's days, or a preset — today, yesterday, 7d, 30d, month,
    last_month, 12mo;
  - `compare` previous|year_ago;
  - `order_by`, `limit` ≤ 1000.

  Everything is allow-listed; a wrong combination answers `400` with a reason. A range inside the raw
  retention reads raw rows (filters allowed). A longer one reads the aggregates: filters there answer
  `400`, hour intervals need a single day. Every query has a deadline. — _Depends on:_ B1, B2
- [x] `B4` CSV: the same request with `"format":"csv"` returns `text/csv`. Cells that start with
  `= + - @` are prefixed with `'` (formula injection). — _Depends on:_ B3
- [x] `B5` `GET /api/sites/{id}/realtime` → `{visitors}`: distinct visitor hashes with a pageview in the
  last 5 minutes. — _Depends on:_ —
- [x] `B6` Tests:
  - raw and aggregate paths give the same numbers for a synthetic month;
  - comparison periods;
  - filters by visit and page dimensions;
  - DST-free and DST time zones for day buckets;
  - validation errors;
  - CSV escaping;
  - backfill idempotence;
  - 25-month purge. — _Depends on:_ B1–B5

### Other
- [x] `T1` SPEC §4i (query contract, history), RUNBOOK (aggregate job), KNOWN_GOTCHAS if any. — _Depends on:_ B1–B5

---

## Files

### Create / modify
~~~
internal/analytics/{domain,application,infrastructure,interfaces/http}/* (query, rollup, realtime)
internal/platform/db/migrations/0012_analytics_daily.sql
docs/SPEC.md, docs/RUNBOOK.md
~~~

### Do NOT touch
- The UI (Change 37); `GET /api/sites/{id}/stats` stays for the current view until 37 replaces it.
- Collection (Change 35), host telemetry, uptime, operator.

---

## Contracts

See `docs/SPEC.md` §4i and the Files list above.

---

## Gate Checks

Fast Gate per task; Full Gate at ship.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- The query service is `application.Queries` beside the site `Service`: its own port and its own
  hourly job, so collection and its tests stay untouched.
- A range across the raw boundary sums aggregates for the older days and raw rows for the rest
  (counts are additive), so today is never an hour stale in a long range. Filters and hour intervals
  answer `400` when any requested day, the comparison included, is older than the raw retention.
- Backfill skips days the raw purge has already cut into, so a partial day is never aggregated.
- Path values always start with `/`, so CSV escaping matters for UTM values (`-` is allowed there).
- Full Gate 13/13 PASS on 2026-10-26.

---

## Commit Message

```
feat(change-36): analytics queries and history — query API, daily aggregates, comparison
```
