# CHANGE 14 — Site Analytics: Backend, Ingest and Snippet

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `14` |
| Slug | `site-analytics-backend` |
| Title | Site Analytics: Backend, Ingest and Snippet |
| Status | `archived` |
| Branch | `feature/14-site-analytics-backend` |

---

## Goal

First half of the v2 "Analytics" backlog item (docs/SPEC.md §4i, §9): cookieless JS-based website
visitor analytics — storage, the public ingest endpoint, the read API, and the tracking snippet
(`/track.js`). No UI yet (Change 15). Architect-approved direction: JS snippet over passive
access-log parsing, because the real dogfood target is a client-side-routed SPA where server logs
would badly undercount navigation.

---

## Design References

<!-- none provided -->

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Data
- [x] `D1` Migrations: `sites (id, name, domain, created_at)`, `pageviews (id, site_id, ts, path,
  referrer_domain, visitor_hash, browser, os, device)`, `pageview_rollups_daily (site_id, day,
  path, unique_visitors, pageviews)` (docs/SPEC.md §4i) — _Depends on:_ —

### Backend
- [x] `B1` Site domain/application service: create (name, domain; domain unique), list, delete
  (cascades pageviews) — _Depends on:_ D1
- [x] `B2` A server-held, persisted daily-salt secret for the visitor hash — generated once,
  stored like the existing WireGuard server key (`internal/telemetry/infrastructure/enrollment.go`
  is the pattern to follow), never exposed via any API — _Depends on:_ D1
- [x] `B3` `POST /api/collect`: validate `site`/`url` (others optional, all capped), derive
  `browser`/`os`/`device` from the `User-Agent` header, reduce `referrer` to hostname, compute
  `visitor_hash` from B2's daily salt, per-IP rate limit
  (`internal/platform/ratelimit`), CORS allowed only when `Origin` matches the site's registered
  domain, always `204` regardless of outcome (docs/SPEC.md §4i) — _Depends on:_ B1, B2
- [x] `B4` Server-side known-bot `User-Agent` filter applied in B3, in addition to "the browser ran
  the JS at all" — _Depends on:_ B3
- [x] `B5` Daily rollup + retention job for `pageviews`/`pageview_rollups_daily`, mirroring the
  existing metric-rollup/purge job's start-up-then-scheduled pattern (§4.4/§4.5) — _Depends on:_ D1
- [x] `B6` Read API (session required): `GET /api/sites`, `POST /api/sites`, `DELETE
  /api/sites/{id}`, `GET /api/sites/{id}/stats?range=today|7d|30d` (docs/SPEC.md §4i) — _Depends on:_ B1, B5
- [x] `B7` `GET /track.js`: static snippet, listens for `pushState`/`replaceState`/`popstate` in
  addition to initial load, posts via `navigator.sendBeacon` (fetch fallback) — _Depends on:_ B3

### Other
- [x] `T1` Real verification: register a site against the live `sre.infraege.ru` deployment,
  confirm a real SPA's client-side route changes each produce a pageview, confirm a known-bot
  User-Agent is filtered, confirm CORS works from the real tracked domain and is rejected from an
  unregistered one — _Depends on:_ B1–B7

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship). -->

---

## Files

### Create / modify
~~~
internal/analytics/           [new] domain/application/infrastructure/interfaces (own bounded context)
internal/platform/db/migrations/  (new tables)
cmd/smotryashchiy/server.go   (wire the new routes)
web/public/ or a Go-served static handler for /track.js (not part of the React app build)
docs/SPEC.md §4i (already drafted)
~~~

### Do NOT touch
- Host telemetry (`internal/telemetry/`), agent, transport — unrelated bounded context
- UI (`web/src/`) — Change 15
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4i (wire contract, privacy construction, storage, read API) and §6 (privacy
NFR). No changes to the existing telemetry/uptime contracts.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: real verification per T1 against the live production
deployment — not a synthetic fixture, since the entire point of the JS-snippet approach is SPA
route coverage, best proven against a real SPA.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Real, live verification (T1):** deployed to `sre.infraege.ru`, registered `infraege.ru` as a
  site via the real API. `/track.js` served correctly; a curl "visitor" (bot User-Agent) was
  correctly dropped (`204`, 0 pageviews stored). In a real browser (Playwriter), the snippet loaded
  from the live external origin onto a real HTTPS page, fired an initial pageview, then two
  `history.pushState` calls (simulating SPA in-app navigation) each fired their own pageview — 3
  distinct paths landing as 3 pageviews under 1 grouped visitor, proving the entire premise this
  change exists for. CORS confirmed live: `Access-Control-Allow-Origin` present only when `Origin`
  matches the registered domain (`infraege.ru`), absent for an unregistered one. Test site and its
  synthetic pageviews were deleted afterward for a clean state.
- **Real-world constraint found during T1, not a bug:** `infraege.ru`'s own CSP
  (`script-src 'self'; connect-src 'self'`) blocks loading `track.js` and its beacon calls when
  actually embedded on that page — verification above used a page without that restriction to
  prove the mechanism, since modifying infraege's own CSP is out of this repo's scope. **Any site
  embedding the snippet needs its CSP (if it has one) to allow this server's origin** in both
  `script-src` and `connect-src`. Worth a line in the `+ add site` dialog's snippet instructions
  (Change 15) and/or `docs/RUNBOOK.md`.

---

## Commit Message

```
feat(change-14): site analytics ingest, storage and tracking snippet
```
