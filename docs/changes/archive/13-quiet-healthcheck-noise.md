# CHANGE 13 — Quiet Healthcheck Noise

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `13` |
| Slug | `quiet-healthcheck-noise` |
| Title | Quiet Healthcheck Noise |
| Status | `archived` |
| Branch | `feature/13-quiet-healthcheck-noise` |

---

## Goal

Second v2-backlog item (docs/SPEC.md §9): the journald and Docker-log event collectors (Change 11)
forward every access-log line, including a monitored process's own routine health-check polling
(every few seconds), which drowns out real signals (UFW blocks, SSH auth, fail2ban bans) in the
live EVENTS list — observed directly on the real production dashboard. Filter out only successful
(2xx) health/readiness-endpoint access-log lines from these two sources; nothing else changes.

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

### Backend
- [x] `B1` A shared `isRoutineHealthCheck(line string) bool` (or similar) in `internal/agent/collect/`:
  matches an HTTP access-log-style line naming a health/readiness path
  (`/health`, `/healthz`, `/ready`, `/health/ready`, case-insensitive) with a 2xx status; anything
  else (non-2xx, unrelated path, non-access-log line) returns false — _Depends on:_ —
- [x] `B2` Journald collector (`journald.go`): drop a line when `isRoutineHealthCheck` matches,
  before it enters the buffer — _Depends on:_ B1
- [x] `B3` Docker-log collector (`docker_logs.go`): same drop, applied per emitted line — _Depends on:_ B1

### Other
- [x] `T1` Real verification on the Stage-7 target VPS (2.26.8.245, already producing this exact
  noise): redeploy, confirm `/api/events` stops receiving new health-check-only lines while other
  events (UFW, fail2ban, non-2xx) keep arriving — _Depends on:_ B2, B3

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship). -->

---

## Files

### Create / modify
~~~
internal/agent/collect/journald.go, docker_logs.go
internal/agent/collect/ (new shared filter, e.g. healthcheck_filter.go)
docs/SPEC.md §4h, §9 (already drafted)
~~~

### Do NOT touch
- fail2ban collector, metrics/checks collectors — untouched by design
- UI (Change 12), server/telemetry code — no wire or API change
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4h (health-check noise filter). No wire, API, or UI change.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: redeploy to the Stage-7 target VPS and confirm via
`/api/events` that new health-check-only lines stop arriving while everything else (UFW blocks,
fail2ban bans, non-2xx requests) keeps flowing — real infrastructure already producing this exact
noise pattern, not a synthetic fixture.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Real, live verification (T1):** redeployed the built agent to the Stage-7 target VPS
  (2.26.8.245) and confirmed via `/api/events` that all 70 events received in the ~7 minutes after
  restart were real signals (SSH sessions, UFW blocks, systemd) — zero health-check-noise lines,
  versus ~80 such lines in a comparable pre-fix window. Real events (UFW, SSH, systemd) kept
  arriving normally throughout, confirming the filter's narrowness didn't drop anything else.
- `/api/events` has no `from`/`to` filter (only `host`/`level`/`limit`, docs/SPEC.md §4.3) — passing
  one is silently ignored rather than rejected; verification above filtered client-side by
  timestamp instead.

---

## Commit Message

```
feat(change-13): filter routine health-check noise from log events
```
