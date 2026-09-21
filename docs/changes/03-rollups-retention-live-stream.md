# CHANGE 03 — Rollups, Retention and Live Stream

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `03` |
| Slug | `rollups-retention-live-stream` |
| Title | Rollups, Retention and Live Stream |
| Status | `active` |
| Branch | `feature/03-rollups-retention-live-stream` |

---

## Goal

Finish Stage 1: keep storage bounded and queryable over long ranges (hourly metric rollups, 30-day
raw / 13-month rollup retention) and push newly accepted records to the browser over an
authenticated WebSocket. See `docs/SPEC.md` §4.3–§4.6. No HTTP ingest, enrollment, agent, UI or
alerting.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Data
- [x] `D1` Migration `0003_rollups.sql`: `metric_rollups_hourly` (PK host, name, labels_json, hour_ts; count, min, max, sum) plus a `rollup_state` marker for catch-up — _Depends on:_ —

### Backend
- [x] `B1` Rollup: repository aggregation of one closed hour from raw metrics (idempotent upsert, zero counted) and a service that catches up from the last rolled hour and re-aggregates the last 24 h, injectable clock — _Depends on:_ D1
- [x] `B2` Retention: bounded-batch purge of raw metrics/checks/events/`ingestion_batches` (30 d) and rollups (13 mo); raw rows deleted only after their hour is rolled up — _Depends on:_ B1
- [x] `B3` Config `SMOTRYASHCHIY_RAW_RETENTION_DAYS` / `SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS` with fail-fast validation — _Depends on:_ —
- [x] `B4` Scheduler: runs rollup then purge at start-up and on hourly/daily tickers, stops on shutdown, logs failures without crashing the server; wired in `server.go` — _Depends on:_ B1, B2, B3
- [x] `B5` Read side: `resolution=hour` for `GET /api/metrics` (`{ts,count,min,max,avg}`, `latest` rejected with hour) — _Depends on:_ B1
- [x] `B6` In-process stream hub: non-blocking fan-out with 64-message per-client buffer, slow client dropped, host/type filters; ingest service publishes only newly accepted records — _Depends on:_ —
- [x] `B7` `GET /api/stream` WebSocket handler (session + same-origin check, ping every 30 s, JSON frames per SPEC §4.6) mounted in `server.go` — _Depends on:_ B6
- [x] `B8` Contract tests: rollup of a zero-only hour, idempotent re-run, late data picked up, purge keeps unrolled raw rows, replay/duplicate never published, slow client cannot stall ingest, cross-origin and unauthenticated upgrade rejected — _Depends on:_ B4, B5, B7

### Other
- [x] `T1` Update `docs/STACK.md` structure/env vars, record new gotchas in `docs/KNOWN_GOTCHAS.md` — _Depends on:_ B8

---

## Files

### Create / modify
~~~
internal/platform/db/migrations/0003_rollups.sql
internal/platform/config/
internal/telemetry/domain/
internal/telemetry/application/      (rollup, retention, scheduler, hub, ingest publish)
internal/telemetry/infrastructure/
internal/telemetry/interfaces/http/  (resolution param, stream handler)
cmd/smotryashchiy/server.go
go.mod / go.sum                      (WebSocket library, check with Context7)
docs/STACK.md
docs/KNOWN_GOTCHAS.md
~~~

### Do NOT touch
- HTTP ingest endpoint, host enrollment, WireGuard, agent code (Stage 2)
- UI, alerting, uptime prober
- `internal/auth` (consume `RequireSession` only), archived changes

---

## Contracts

See `docs/SPEC.md` §3–§4 and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: start the server, log in, open `/api/stream` with a
session (upgrade succeeds) and without one (rejected), and confirm
`/api/metrics?resolution=hour` answers `200` with an empty list.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- Raw purge cutoff is `min(now - TTL, rolled_through - 24h)` and is skipped until the first rollup
  completed, so a rollup can always be recomputed from complete raw data (see KNOWN_GOTCHAS).
- `resolution=hour` responds with key `rollups` (different item shape than `metrics`).
- The slow-client drop is asserted at hub level; the socket-level test only proves ingest is never
  stalled, because kernel buffers make a real overflow non-deterministic.
- Ingest still has no HTTP endpoint; the publisher is attached to the read service instance in
  `server.go` and Stage 2 must reuse that same instance (or `hub`) for the ingest handler.

---

## Commit Message

```
feat(change-03): rollups, retention, live stream
```
