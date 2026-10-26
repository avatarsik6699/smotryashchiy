# CHANGE 38 — Console performance: compression, connection pool, bounded reads, fewer renders

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `38` |
| Slug | `perf-console` |
| Title | Console performance: compression, connection pool, bounded reads, fewer renders |
| Status | `archived` |
| Branch | `feature/38-perf-console` |

---

## Goal

The 2026-10-26 audit of sre.infraege.ru (report: https://claude.ai/artifact/58qZHqjzVJFuhv5KhG7mCq)
measured on v0.5.0:
- Lighthouse mobile 68 (LCP 6.2 s), with 800 KiB sent uncompressed;
- API reads up to 5.8 s on the server, queued behind one SQLite connection and whole-table scans;
- 5.8 s of JavaScript per idle minute, because every stream frame re-rendered the dashboard.

Architect decisions of 2026-10-26: fix every high and medium finding in one change; measure before
and after on production (sign-in through Playwriter with the athanor password); VACUUM the
production database at the release, about a minute of downtime.

---

## Backlog

### Backend
- [x] `B1` Compression: precompressed `.br`/`.gz` assets chosen by `Accept-Encoding`; gzip for JSON
  and `track.js` from 1 KB. — _Depends on:_ —
- [x] `B2` SQLite pool of 4, `_txlock=immediate`, `synchronous=NORMAL`, 32 MB cache, 128 MB mmap,
  `PRAGMA optimize` at start-up and daily. — _Depends on:_ —
- [x] `B3` Bounded reads:
  - `events?from=` with a `(level, ts)` index;
  - `check_latest` kept by triggers;
  - the newest uptime result by a key seek per target;
  - cross-host history through the series index;
  - a metrics limit keeps the newest samples. — _Depends on:_ B2
- [x] `B4` Purge hourly with a pause between batches; ingest with prepared statements. — _Depends on:_ —
- [x] `B5` Collect caches the site list and the salt base; a raw-only breakdown is cut by SQL
  `ORDER BY … LIMIT`. — _Depends on:_ —
- [x] `B6` `admin vacuum [--keep]`: `VACUUM INTO` and swap, refused while the server runs. — _Depends on:_ —
- [x] `B7` HTTP `IdleTimeout` 2 min. — _Depends on:_ —

### Frontend
- [x] `F1` Stream frames publish once per animation frame; charts redraw only on new points;
  `useNow` slower off Monitoring and paused in a hidden tab. — _Depends on:_ —
- [x] `F2` Hidden tabs skip the full refresh, the documents and the realtime polls and catch up once;
  error events ask for the last hour. — _Depends on:_ B3
- [x] `F3` Code splitting: the sign-in screen without the dashboard; the views, the dialogs and the
  lesson texts load on first use; the bundle budget counts brotli bytes (entry ≤ 80 KB, all ≤ 200 KB).
  — _Depends on:_ B1
- [x] `F4` Analytics panels already ignore the KPI metric; no change needed. — _Depends on:_ —

### Other
- [x] `T1` SPEC §4.2, §4.3, §4.5, §4d, §4f, §5; STACK budget row; RUNBOOK vacuum and slowness;
  KNOWN_GOTCHAS (VACUUM in memory, deferred transactions). — _Depends on:_ B1–F3

---

## Files

### Create / modify
~~~
internal/platform/{httpserver/compress.go,db/sqlite.go,backup/vacuum.go}
internal/platform/db/migrations/0013_perf.sql
internal/telemetry/**, internal/uptime/infrastructure/store.go, internal/analytics/**
cmd/smotryashchiy/{admin.go,backup.go,server.go}
web/embed.go, web/vite.config.ts, web/src/** (store, chart, useNow, lazy views, lessons split)
scripts/bundle-budget.sh, docs/*
~~~

### Do NOT touch
- The agent and its wire format; the analytics query contract of Change 36.

---

## Contracts

See `docs/SPEC.md` §4.2–§4.5, §4d, §4f and §5.

---

## Gate Checks

Fast Gate per task; Full Gate at ship; production measurements before (done) and after the release.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- The telemetry rollup keeps its 24 h re-aggregation window: an agent spool holds up to 5000 batches
  (about 14 h at 10 s), so late data can reach that far. At the current volume it costs little.
- The stream still triggers a full load on its first open: the load closes the gap between the first
  request and the socket, where frames could be missed.
- The monitoring path is about 145 KB brotli instead of 170 KB, not below 120 KB: React DOM and uPlot
  stay in it, since the host rows draw sparklines at once. The sign-in screen dropped to 71 KB.
- A plain `VACUUM` was not an option: with `temp_store=MEMORY` it would build a copy as large as the
  database in memory.

---

## Commit Message

```
perf(change-38): console performance — compression, connection pool, bounded reads, fewer renders
```
