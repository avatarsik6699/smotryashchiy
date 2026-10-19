# CHANGE 26 — Tracker delivery, private paths and drop counters

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `26` |
| Slug | `tracker-delivery` |
| Title | Tracker delivery, private paths and drop counters |
| Status | `archived` |
| Branch | `feature/26-tracker-delivery` |

---

## Goal

Fix the analytics delivery defects found by the 2026-10-14 monitoring audit before the tracked
site (infraege → kodumka.ru) launches: a refused `sendBeacon` loses the pageview (A1), and every
drop or storage failure is invisible because collect always answers 204 (A2). Add the
`data-exclude` attribute the tracked site needs so in-app navigation to its account pages is
never recorded (SPEC §4i).

---

## Backlog

### Backend
- [x] `B1` `track.js`: `data-exclude="/a,/b"` — a path equal to a listed prefix or under it is
  not sent; leaving and returning to a public page still counts — _Depends on:_ —
- [x] `B2` `track.js` (A1): a `sendBeacon` that returns `false` or throws falls back to
  `fetch(..., {keepalive: true})`; one send per pageview — _Depends on:_ —
- [x] `B3` Collect (A2): in-memory counters per outcome (`stored`, `rate_limited`,
  `malformed`, `invalid`, `bot`, `unknown_site`, `origin_mismatch`, `error`); a storage error is
  logged without IP or User-Agent; the public response stays 204 — _Depends on:_ —
- [x] `B4` `GET /api/collect/stats` (session) returns the counters since start — _Depends on:_ B3

### Other
- [x] `T1` SPEC §4i, RUNBOOK (reading the counters) — _Depends on:_ B1, B2, B4
- [x] `T2` Dependency audit (found by the Full Gate): Go 1.26.9 (`net/http`, `crypto/tls`,
  `net/textproto` advisories) in `go.mod`, the build image and STACK; `golang.org/x/net` v0.60.0 —
  _Depends on:_ —

---

## Files

### Create / modify
~~~
internal/analytics/interfaces/http/track.js, track_test.go
internal/analytics/application/service.go, service_test.go
internal/analytics/interfaces/http/collect.go, collect_test.go, handlers.go
docs/SPEC.md, docs/RUNBOOK.md
~~~

### Do NOT touch
- Storage schema and migrations; the UI (`web/`); `docs/changes/archive/`

---

## Contracts

See `docs/SPEC.md` §4i and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../../STACK.md) — this section only records
> change-specific overrides.

None

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- Full Gate 2026-10-19: gofmt/vet, `go mod verify`, web ci/typecheck/tests/build, bundle budget
  (183.5 KB of 200), `go test ./...`, build, Gitleaks and govulncheck — all PASS after T2. The
  first govulncheck run failed on a sum.golang.org EOF and was rerun.
- Counters are process memory, not storage: the change adds no migration.

---

## Commit Message

```
feat(change-26): tracker fallback, excluded paths, collect counters
```
