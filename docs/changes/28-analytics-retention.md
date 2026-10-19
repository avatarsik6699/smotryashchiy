# CHANGE 28 — Analytics retention apart from raw telemetry

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `28` |
| Slug | `analytics-retention` |
| Title | Analytics retention apart from raw telemetry |
| Status | `archived` |
| Branch | `feature/28-analytics-retention` |

---

## Goal

Raw host metrics filled the monitoring server's 20 GB disk (6.6 GB database, 30 days of 10-second
samples). The architect shortens raw telemetry to 14 days on production (2026-10-19) but site
pageviews must keep their 30-day stats range, so they get their own TTL (SPEC §4i, §4.5).

---

## Backlog

### Backend
- [x] `B1` `SMOTRYASHCHIY_ANALYTICS_RETENTION_DAYS` (default 30) drives the pageview purge
  instead of the raw TTL — _Depends on:_ —

### Other
- [x] `T1` SPEC §4i, STACK, RUNBOOK (production uses a 14-day raw TTL from v0.4.0) — _Depends on:_ B1

---

## Files

### Create / modify
~~~
internal/platform/config/config.go, config_test.go, cmd/smotryashchiy/server.go
docs/SPEC.md, docs/STACK.md, docs/RUNBOOK.md
~~~

### Do NOT touch
- Storage schema; `docs/changes/archive/`

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

- The production compose gets `SMOTRYASHCHIY_RAW_RETENTION_DAYS: "14"` when v0.4.0 is deployed with
  the general release; until then the disk has room (56 % used) and the database stops growing at
  the 30-day steady state.

---

## Commit Message

```
feat(change-28): pageviews keep their own retention
```
