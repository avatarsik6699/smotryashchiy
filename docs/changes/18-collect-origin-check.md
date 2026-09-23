# CHANGE 18 — Store a beacon only from the site's own origin

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `18` |
| Slug | `collect-origin-check` |
| Title | Store a beacon only from the site's own origin |
| Status | `active` |
| Branch | `feature/18-collect-origin-check` |

---

## Goal

The tracked site's own builds carry the snippet, so its local, CI and Lighthouse runs sent real
beacons into production. For infraege.ru, 72 of the first 74 stored pageviews came from test runs
before its first production deploy (2026-09-23 09:57 UTC): dev-server checks and two Lighthouse
runs of the infraegev2 Full Gate. Lighthouse reports a mobile Chrome User-Agent, so the bot filter
cannot catch it. Every future gate or dev session would repeat this. This change stores a beacon
only when the request's `Origin` hostname is the site's `domain` or `www.` plus it (SPEC.md §4i).
The endpoint still always answers `204`. The architect decided to keep the 72 existing test rows
(2026-09-23); nothing is deleted.

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
- [x] `B1` `application.Service.Collect` takes the request's origin hostname and drops the beacon
      unless it equals the site's domain or `www.` + domain (case-insensitive). A missing, `null`
      or unparsable origin is dropped. — _Depends on:_ —
- [x] `B2` `interfaces/http/collect.go` passes the `Origin` header's hostname. `setCORS` keeps its
      behavior. — _Depends on:_ B1
- [x] `B3` Tests. Service level: matching, `www.`, mismatched (`localhost`, `127.0.0.2`, other
      domain) and empty origin. HTTP level: a matching-Origin beacon is stored, a mismatched or
      absent one is not, and both answer `204`. Existing tests move to a matching Origin where
      they expect storage. — _Depends on:_ B1, B2

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: a tracked site's own builds post real beacons, and
      Lighthouse's mobile-Chrome User-Agent bypasses the bot filter. SPEC.md §4i was updated by
      `/plan`. — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/analytics/application/service.go
internal/analytics/application/service_test.go
internal/analytics/interfaces/http/collect.go
internal/analytics/interfaces/http/collect_test.go
docs/KNOWN_GOTCHAS.md
docs/SPEC.md                                   (§4i, done by /plan)
~~~

### Do NOT touch
- Existing pageview rows. The architect kept the 72 pre-deploy test rows; no cleanup migration.
- `track.js`, the dashboard, the storage schema.

---

## Contracts

See `docs/SPEC.md` §4i and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../STACK.md) — this section only records
> change-specific overrides.

```bash
# After deploy: a real browser on https://infraege.ru stores a pageview. A beacon from the
# infraegev2 dev server (http://localhost:3000) or a curl with no/foreign Origin stores none.
```

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

None

---

## Commit Message

```
fix(change-18): store a pageview only from the tracked site's own origin
```
