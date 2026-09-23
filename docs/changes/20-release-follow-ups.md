# CHANGE 20 — Follow-ups from the Change 19 release

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `20` |
| Slug | `release-follow-ups` |
| Title | Follow-ups from the Change 19 release |
| Status | `active` |
| Branch | `feature/20-release-follow-ups` |

---

## Goal

Verifying v0.2.3 on sre.infraege.ru showed that the cross-origin guard runs inside the session check,
not outside it as SPEC §4d and the code comment state. A cookieless foreign write answers `401`
instead of `403`. Security was not affected, but the docs and tests were wrong. This change fixes the
order and closes the smaller items the architect chose from the audit: the unbounded login limiter,
browser names that matter for a Russian audience, snippet fields nobody stores, and a rollup table
nobody reads. Contract: `docs/SPEC.md` v1.20 (§3 Auth, §4d, §4i).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     Group items by area (Backend / Frontend / Infra / Data, etc.).
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. Mark removed items as ~~BN~~ (removed).
     New items always take the next unused ID in their group, appended at the end. -->

### Backend
- [x] `B1` Middleware order: fix `httpserver.Server.Use`'s doc to state the actual order (the last
      added runs closest to Mux), register the cross-origin guard so it runs before `RequireSession`,
      and fix the `server.go` comment. Test the real chain: build it through `httpserver.Server` with
      the same `Use` calls as `server.go` (a shared helper both use), and assert a cookieless foreign
      write gets `403`. — _Depends on:_ —
- [x] `B2` Login limiter (§3 Auth): key clients with `ratelimit.ClientKey` (IPv6 `/64`), and bound
      the attempts map at 10 000 clients by dropping expired entries first, then the least recently
      seen. Tests: two addresses in one `/64` share a failure count; the map stays bounded under a
      flood. — _Depends on:_ —
- [x] `B3` UA parsing (§4i): name Yandex Browser; report `CriOS/` as Chrome and `FxiOS/` as
      Firefox; treat `Chrome-Lighthouse` as a bot. Table tests with real UA strings for each case
      plus the existing ones. — _Depends on:_ —
- [x] `B4` `track.js` (§4i): stop sending `title`, `screen`, `language`. Harness asserts the body
      keys are exactly `site`, `url`, `referrer`. The server keeps decoding old beacons that carry
      the extra fields (test). — _Depends on:_ —

### Data
- [x] `D1` Drop `pageview_rollups_daily`: migration `0007` (`DROP TABLE IF EXISTS`), and remove
      `RollupDay` from the repository port and store plus the rollup step of `Service.Run`, keeping
      the purge job. Tests: the migration count, a fresh migrate has no such table, and the store and
      service tests pass without the rollup. A pre-Change-20 backup bundle must still restore and
      migrate forward. — _Depends on:_ —

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: `Server.Use` order (last added is innermost), and why a
      hand-built middleware chain in a test hid it. — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/platform/httpserver/server.go, server_test.go          (B1)
cmd/smotryashchiy/server.go (+ a shared chain helper)          (B1)
internal/auth/interfaces/http/*_test.go                         (B1)
internal/auth/interfaces/http/login_limiter.go, tests           (B2)
internal/analytics/application/service.go, service_test.go      (B3, D1)
internal/analytics/interfaces/http/track.js, track_test.go, collect_test.go   (B4)
internal/platform/db/migrations/0007_drop_pageview_rollups.sql  (D1)
internal/analytics/infrastructure/store.go, store_test.go       (D1)
internal/platform/db tests / backup tests touching migration count   (D1)
docs/KNOWN_GOTCHAS.md                                           (T1)
docs/SPEC.md                                                    (§3, §4d, §4i — done by /plan)
~~~

### Do NOT touch
- Existing pageview rows; metric rollups (`metric_rollups_hourly`) and their TTL setting.
- The collect endpoint's origin, CORS and rate-limit behavior from Changes 18–19.
- `deploy/`, off-host backup (architect: not now), alerting, infraegev2.

---

## Contracts

See `docs/SPEC.md` §3, §4d, §4i and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../STACK.md) — this section only records
> change-specific overrides.

After release, on production: a cookieless `POST /api/auth/logout` with
`Origin: https://evil.infraege.ru` answers `403` (it answered `401` on v0.2.3); the server log shows the
migration count `7` and no `analytics maintenance job failed`.

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work [XX] review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

- B4 removed `title`/`screen`/`language` from `domain.Beacon` too, not only from `track.js`: the JSON
  decoder ignores unknown keys, so older snippets (cached up to an hour) still post accepted beacons;
  `TestCollectStillAcceptsBeaconsFromOlderSnippets` pins that.
- D1 verified with real processes: a v0.2.3 bundle (6 migrations) restored by the new binary migrates
  to 7 on start, keeps its pageviews and has no `pageview_rollups_daily`.

---

## Commit Message

```
fix(change-20): guard order, login limiter bound, UA names, no rollups
```
