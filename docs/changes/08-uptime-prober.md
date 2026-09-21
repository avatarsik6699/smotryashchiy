# CHANGE 08 — Uptime Prober

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `08` |
| Slug | `uptime-prober` |
| Title | Uptime Prober |
| Status | `active` |
| Branch | `feature/08-uptime-prober` |

---

## Goal

Stage 4 (without alerting): the server itself probes operator-defined targets (HTTP, TCP, TLS with
certificate expiry), stores each result, streams it live and shows an UPTIME ledger on the single
dashboard page where targets are added and removed. See `docs/SPEC.md` §4e, §4.6 and §5. No alert
rules, notifications or Telegram (deferred), no per-target pages, no retries or smoothing.

---

## Design References

`docs/reference/DESIGN.md` and the predecessor screenshots (`uptime-http` source rows with latency and
sparkline). The UPTIME ledger reuses the HOSTS ledger vocabulary (state text, hairlines, mono, sparkline).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Data
- [x] `D1` Migration `0005_uptime.sql`: `uptime_targets` (unique name) and `uptime_results` (cascade delete, `ts` index for purging) — _Depends on:_ —

### Backend
- [x] `B1` Domain: target kinds and validation (name, `http` URL rules incl. no credentials, `host:port` for tcp/tls, interval 30–3600, defaults) returning field-addressed `apierror.Invalid` — _Depends on:_ —
- [x] `B2` Checker: http (GET, ≤ 5 redirects, 200–399, body unread), tcp, tls with full certificate verification; 10 s timeout; latency and certificate `NotAfter`; injectable dialer/TLS roots/clock; tests with `httptest` (ok, 500, redirect loop, timeout, refused, wrong cert, expired cert) — _Depends on:_ B1
- [x] `B3` Repository: targets CRUD (limit 50, unique name), insert result, newest result per target, last-hour latency history, delete cascade, purge — _Depends on:_ D1, B1
- [x] `B4` Prober scheduler: one schedule per target with jittered first run, ≤ 8 checks in flight, immediate effect of create/delete, periodic resync, clean stop; wired in `server.go`; publishes each result to the hub — _Depends on:_ B2, B3
- [x] `B5` API `GET/POST /api/uptime`, `DELETE /api/uptime/{id}` per SPEC §4e (`400` invalid, `409` duplicate or limit, `204` delete) — _Depends on:_ B3, B4
- [x] `B6` Stream frames `{"type":"uptime","target_id","record"}` from the hub; `type=uptime` filter, `host` filter ignores them; SPEC §4.6 stays in sync — _Depends on:_ B4
- [x] `B7` Retention: uptime results purged with the raw TTL independently of the rollup gate — _Depends on:_ B3
- [x] `B8` Contract tests end to end: create target → probe hits real test servers → result appears in the API and on the stream; failing target shows `ok=false` with the error and no latency (not 0); delete stops probing and removes results; limit and duplicate name; session gating; purge keeps fresh results — _Depends on:_ B5, B6, B7

### Frontend
- [x] `F1` Types, store and stream: load `/api/uptime`, apply `uptime` frames, state derivation (UP/DOWN/STALE/NEW by interval), latency series, TLS days, `—` for unknown; unit tests — _Depends on:_ B5, B6
- [x] `F2` UPTIME ledger section: state text, name + target, latency value with sparkline, TLS days text, age, inline remove confirmation (no modal), empty state that teaches adding a target — _Depends on:_ F1
- [x] `F3` Add-target dialog on Base UI `Dialog` + `Field` + a radio group for the kind, interval field, specific errors (400/409/limit/network) — _Depends on:_ F1
- [x] `F4` STATUS strip counts for targets (up/down, unknown excluded from both), keyboard and axe tests for the new section and dialog — _Depends on:_ F2, F3

### Other
- [x] `T1` Docs (STACK.md, KNOWN_GOTCHAS, SPEC touch-ups) and real verification recorded in Implementation Notes: real server with real targets (a local HTTP server, a TCP port, a real HTTPS site for certificate days, a closed port); kill and restart a local target and watch `DOWN` then `UP` live; Playwriter at 360/1280 px with the chart layout contract check; add and remove through the UI; console judged in a clean profile — _Depends on:_ B8, F4

---

## Files

### Create / modify
~~~
internal/platform/db/migrations/0005_uptime.sql
internal/uptime/                     (domain, application, infrastructure, interfaces/http)   [new]
internal/telemetry/application/      (hub: uptime frames), maintenance (purge hook)
internal/telemetry/interfaces/http/  (stream: uptime frames)
cmd/smotryashchiy/server.go
web/src/                             (domain/data/components: uptime)
docs/STACK.md, docs/KNOWN_GOTCHAS.md, docs/SPEC.md (touch-ups only)
~~~

### Do NOT touch
- Host telemetry contract, agent, transport, enrollment
- Alerting, Telegram, per-target detail pages
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4e, §4.6, §5.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: start the server with the built UI, add an HTTP target
pointing at a local test server through the UI, see it become `UP` with a latency within one interval,
stop the test server and see `DOWN` with its error, start it again and see `UP`, then remove the target.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Design decisions kept from the plan:** uptime results live in their own tables (a target is not a host);
  state is derived (UP/DOWN/STALE/NEW) and never stored; no retries or smoothing; latency is recorded whenever the
  target answered (SPEC §4e wording corrected from "a failed check has no latency": an HTTP 500 answered);
  the stream's `host` filter excludes uptime frames.
- **Scheduler**: one goroutine per target reconciled on create/delete and every minute; a semaphore caps probes in
  flight at 8 (test with 20 targets: peak between 2 and 8); a result is dropped if its target was deleted mid-probe;
  results are purged with the raw TTL from the same loop, independent of the rollup gate.
- **Real verification (T1)** with a real server, a local HTTP server, a local TCP server, a real HTTPS site
  (`example.com`, `TLS 36d`), a TLS handshake target and a closed port; targets were added through the UI dialog
  (5 in a row). Within one interval: `UP` with latency (2 ms / 98 ms / 145 ms), the closed port `DOWN` with
  `timeout after 10s`, STATUS `PROBES UP 4 / DOWN 1`. Killing the local web server -> `DOWN` (`dial tcp ...:
  connection refused`) after 3 s; restarting it -> `UP` on the next 30 s probe, live, no reload. Removal through the
  inline confirmation removed the row and the counters. Layout contract in the real dashboard: dark/light x
  360/768/1280/1920 px, no horizontal scroll, all charts drawn; a ~100 ms transient after a shrink (clipped) led to a
  synchronous `setSize` in the ResizeObserver callback. Console judged in a clean profile: 0 messages.
- Not observed live: STALE for a target (unit-tested), and an expired certificate (unit- and contract-tested).

---

## Commit Message

```
feat(change-08): uptime prober with http/tcp/tls targets and dashboard ledger
```
