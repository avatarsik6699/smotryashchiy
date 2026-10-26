# CHANGE 37 — Analytics dashboard: KPIs, trend with comparison, breakdown panels, filters

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `37` |
| Slug | `analytics-dashboard` |
| Title | Analytics dashboard: KPIs, trend with comparison, breakdown panels, filters |
| Status | `archived` |
| Branch | `feature/37-analytics-dashboard` |

---

## Goal

Stage S4 of the 2026-10-14 plan, the last of three changes (35 collection, 36 queries). The analytics
view becomes one site's dashboard over the Change 36 query API, built to the approved mockup
(https://claude.ai/artifact/MJMajdhMMHDpMiaFhEapiq, v5, 2026-10-26).

Architect decisions of 2026-10-26: English labels like the rest of the console; restrained colour —
green only as the console accent and the live status; the current period blue, the comparison a grey
dashed line, neutral share bars; a delta is green when the metric got better and red when worse, the
arrow follows the sign (lower bounce rate is better); filters and comparison without URL state.

---

## Backlog

### Frontend
- [x] `F1` Toolbar: site picker, period (today, yesterday, 7 / 30 days, this / last month, 12 months),
  comparison (off, previous period, a year ago), filter chips with remove, current visitors (polled
  every 30 s). — _Depends on:_ —
- [x] `F2` KPI row: visitors (visitor-days over several days), visits, pageviews, views per visit,
  bounce rate, visit duration; the delta against the comparison with the arrow by sign and the colour
  by better/worse plus a hidden «better/worse» label. A KPI picks the chart's metric. — _Depends on:_ F1
- [x] `F3` Trend chart (SVG): the current period blue with a light area, the comparison grey dashed,
  recessive grid, interval switch (hour for one day; day/week; month for 12 months), crosshair and
  tooltip by pointer and arrow keys, an accessible name. — _Depends on:_ F2
- [x] `F4` Four panels with tabs — Sources (channels, sources, campaigns), Pages (top, entry, exit),
  Locations (countries, regions), Devices (screen, browser, OS; with a browser or OS filter the tab
  shows its versions). Neutral share bars; a row click adds an `is` filter to every panel; «details»
  opens up to 100 rows with all metrics and a CSV download. — _Depends on:_ F1
- [x] `F5` States: loading, empty site list (add a site), no data yet, query errors (a filter on an
  aggregate range shows the server's reason with «remove filters»); the footnote about the site's
  timezone, visitor-days and the DB-IP attribution. Phone layout from the mockup. — _Depends on:_ F1–F4
- [x] `F6` Tests (Vitest + axe): delta colour/arrow rules, formatting, panel filter flow, chart
  keyboard tooltip, CSV download request. — _Depends on:_ F1–F5

### Backend
- [x] `B1` Remove the replaced `GET /api/sites/{id}/stats` with its service, store and tests. — _Depends on:_ F1–F5

### Other
- [x] `T1` SPEC §5 (analytics view), §4i (stats removed), the guide lesson about analytics. Playwriter
  check of desktop 1440 and phone 390 on the local server. — _Depends on:_ F1–F6, B1

---

## Files

### Create / modify
~~~
web/src/components/Analytics/* (dashboard, toolbar, KPI row, trend chart, panels, details dialog)
web/src/data/useAnalytics.ts, web/src/domain/analytics.ts (+ tests), web/src/domain/types.ts
internal/analytics/* (stats removal), docs/SPEC.md, web/src/guide/lessons.ts
~~~

### Do NOT touch
- The query API contract of Change 36, collection (Change 35), other views.

---

## Contracts

See `docs/SPEC.md` §4i (query API) and §5 (analytics view).

---

## Gate Checks

Fast Gate per task; Full Gate at ship; Playwriter check of the analytics view at 1440 and 390.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- The trend is a hand-built SVG, not uPlot: it needs the area, the dashed comparison and a keyboard
  tooltip, and the dashboard's buckets are few. Light theme gets its own series steps; the grey
  comparison fails the validator's chroma floor by design and carries the dashed line as its second
  encoding.
- A filtered query over days older than the raw retention answers `400`; the toolbar notice gives the
  reason once with «remove filters», panels and the trend say «not available with these filters».
- Version rows name their parent while the parent is filtered («Chrome 131»).
- Pageviews before Change 35 have no visit, so the dashboard starts with the release of 35.
- Playwriter on a local server with synthetic data (100 days, 60-day raw retention): 1440 and 390
  layouts, browser → versions, 12 months over aggregates, the filter notice and recovery, details.
- Bundle 203.9 of 204.8 KB gzip: the next UI change needs a split or a budget decision.
- Full Gate 13/13 PASS on 2026-10-26.

---

## Commit Message

```
feat(change-37): analytics dashboard — KPIs, trend with comparison, breakdown panels, filters
```
