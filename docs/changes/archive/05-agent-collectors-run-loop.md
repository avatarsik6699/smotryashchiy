# CHANGE 05 — Agent Collectors and Run Loop

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `05` |
| Slug | `agent-collectors-run-loop` |
| Title | Agent Collectors and Run Loop |
| Status | `archived` |
| Branch | `feature/05-agent-collectors-run-loop` |

---

## Goal

Finish Stage 2: `agent run` collects host metrics (CPU, RAM, swap, disk, network, load, uptime) on
an interval, keeps one persistent WireGuard tunnel, and never loses data across outages thanks to a
durable spool with retry. After this change a real host is monitored end to end into the server
(read API and live stream). See `docs/SPEC.md` §4c. No UI, uptime prober, Docker/logs/fail2ban
(Stage 5), alerting (deferred).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Backend
- [x] `B1` Collector model and `/proc` collectors with an injectable proc root: cpu (delta between ticks, first tick emits nothing), memory, swap, load, uptime — fixture-based tests, no fabricated zeros on read errors — _Depends on:_ —
- [x] `B2` Disk collector (`/proc/mounts` allowlist, one per device, ≤ 16, `statfs`, `df`-style percent) and network collector (`/proc/net/dev`, excludes `lo`) — _Depends on:_ B1
- [x] `B3` Batch builder: samples → §4.1 wire batch JSON (finite values, zero kept, valid names/labels), verified against `domain.DecodeBatch` and `Normalize` in tests — _Depends on:_ B1, B2
- [x] `B4` Spool: durable bounded FIFO of `{key, batch}` (dir `0700`, files `0600`, atomic writes, 5000 batches / 64 MiB, oldest dropped with counter, survives restart) — _Depends on:_ —
- [x] `B5` Persistent tunnel session and sender: reuse one tunnel, classify responses (200 remove; 400/413 drop and log; 401/429/5xx/network keep), backoff 1–60 s with jitter, rebuild the tunnel after 3 failures; refactor `agent.Push` onto the session — _Depends on:_ B4
- [x] `B6` `agent run` command: interval flag (5 s–5 min), tick → collect → build → spool → drain, graceful SIGINT/SIGTERM (in-flight batch spooled), Linux-only guard, structured logging — _Depends on:_ B3, B5
- [x] `B7` End-to-end contract tests against the real in-process server: metrics arrive incl. a measured zero; outage then recovery delivers every spooled batch exactly once and in order (idempotency keys); a permanently rejected batch is dropped without blocking the queue; spool bound and restart resume — _Depends on:_ B6

### Other
- [x] `T1` Update `docs/STACK.md`, `docs/KNOWN_GOTCHAS.md`; real-process verification recorded in Implementation Notes (agent against a real server, outage/recovery, values sanity-checked against `free`/`df`/`/proc`) — _Depends on:_ B7

---

## Files

### Create / modify
~~~
internal/agent/collect/        (collectors, proc parsing)              [new]
internal/agent/spool/          (durable FIFO)                          [new]
internal/agent/                (session, sender, runner, batch builder)
cmd/smotryashchiy/agent.go     (agent run)
docs/STACK.md, docs/KNOWN_GOTCHAS.md
~~~

### Do NOT touch
- Server-side ingest/enrollment contracts (`docs/SPEC.md` §4.1, §4b) unless a defect is found — then
  record it as a Backlog item first
- Docker, journald, fail2ban collectors (Stage 5); UI; uptime prober; alerting
- `internal/auth`

---

## Contracts

See `docs/SPEC.md` §4.1, §4b–§4c and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: start the server, enroll a real agent, run
`agent run --interval 5s` for ~20 s and confirm all catalog metrics appear via
`GET /api/metrics` with sane values; stop the server for ~15 s, restart it, and confirm the agent
delivers the buffered batches (no gaps in timestamps, no duplicates).

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Verified with real processes** (server, `admin host create`, `agent enroll`, `agent run --interval 5s`):
  all 16 catalog names arrived. Values matched the system: memory/swap totals and swap used exactly
  as `free -b`, `disk.used_bytes` within one block of `df -B1 /` (93.53 % ~ `df` 94 %), load and
  uptime as `/proc`. `memory.used_bytes` intentionally differs from `free`'s "used" column
  (`total - MemAvailable`, see KNOWN_GOTCHAS).
- **Outage with a real killed server** (18 s down): the agent kept collecting into the spool,
  delivered it after the restart within ~1 s of the server coming back (tunnel peers restored,
  `peers=1`), 0 duplicate identities, no missing ticks. Two 8.34 s spacings in the series were
  environment stalls, proven with a parallel watchdog (see KNOWN_GOTCHAS).
- `Run` accepts any positive interval so tests can tick fast; the 5 s..5 min operator range is enforced
  only by `agent run` via `ValidateInterval`.
- Collector errors never emit zeros: an unreadable source omits its metrics until it recovers. A tick
  with nothing measurable produces no batch. The first CPU tick emits nothing (no interval yet).
- The spool is written before any send, so shutdown needs no flush: unsent batches simply stay on disk.
- Outage recovery latency is bounded by the retry backoff (1 s doubling to 60 s), not by the tick.

---

## Commit Message

```
feat(change-05): agent collectors, run loop, offline spool
```
