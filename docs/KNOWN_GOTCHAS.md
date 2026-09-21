# Known Gotchas

> Project memory file. Capture recurring pitfalls that repeatedly waste time during coding,
> testing, or deploys. Add only issues likely to happen again; prefer concrete symptoms, root
> cause and the shortest reliable fix; remove entries that stop being relevant.

## Gotcha Log

### Producer-side lessons inherited from the predecessor (sre-kit)

Carry these into the first contract tests instead of rediscovering them in production:

- **Zero is a value.** A metric of `0` (idle container CPU) must be emitted and stored; never let
  "omit if zero" serialization drop it, and never let a Metric-only field leak into Check records.
- **Timestamps come from the producer** and are validated: reject observations more than five
  minutes in the future; replayed overlapping records are stored once, not re-alerted.
- **Unknown must stay unknown.** No data, stale data and healthy data are three distinct UI states.

### SQLite `INSERT OR IGNORE` swallows constraint violations other than duplicates

- **Symptoms**: a record that violates a `CHECK` or `NOT NULL` constraint is silently skipped and
  counted as a "duplicate"; no error, no data.
- **Root cause**: `OR IGNORE` applies to every constraint class, not only uniqueness.
- **Fix**: dedupe with `INSERT ... ON CONFLICT DO NOTHING`, which only skips uniqueness conflicts
  and still fails loudly on `CHECK`/`NOT NULL`. Covered by
  `TestStoreFailureMidBatchRollsBackEverything`.

### Hijacked WebSocket connections outlive `http.Server.Shutdown`

- **Symptoms**: graceful shutdown waits out its timeout, or stream clients stay connected after the
  server is told to stop.
- **Root cause**: `Shutdown` does not track hijacked connections, and `websocket.Accept` hijacks.
- **Fix**: `telemetry/application.Hub.Close()` is called before `srv.Shutdown`; every stream handler
  exits when its subscription channel closes. New long-lived handlers must do the same.

### Raw purge is gated on rollups and the 24 h re-aggregation window

- **Symptoms**: raw rows older than the TTL are still present; or, if the gate were removed, a
  rollup for a late-data hour would be recomputed from partially purged raw data and shrink.
- **Root cause**: `Maintenance.Purge` deletes raw rows only before `min(now - TTL, rolled_through -
  24h)` and does nothing raw until the first rollup finished. A permanently failing rollup therefore
  stops raw purging (logged as `telemetry maintenance job failed`).
- **Fix**: fix the rollup error; do not loosen the gate.

### `INSERT ... SELECT ... ON CONFLICT` needs a `WHERE`

- **Symptoms**: SQL parse ambiguity when an upsert takes its rows from a `SELECT`.
- **Fix**: keep an explicit `WHERE` (`RollupHour` has one) on any `INSERT ... SELECT ... ON CONFLICT`.

### Two WireGuard handshakes from one key within ~20 ms stall for 5 s

- **Symptoms**: a fresh tunnel from an already-known peer key occasionally takes exactly ~5 s to
  connect (17 % of back-to-back pushes in a loop); the verbose log shows
  `ConsumeMessageInitiation: handshake replay` on the server.
- **Root cause**: `wireguard-go` rounds handshake timestamps to ~16.7 ms (anti-fingerprinting) and
  rate-limits initiations per peer to one per 20 ms. A second initiation inside that window is
  treated as a replay and only retried after the 5 s rekey timeout.
- **Fix**: none needed for a long-lived agent tunnel. Code or tests that create fresh tunnels for the
  same key back to back must space them apart (tests use `handshakeSpacing` = 40 ms).
  `transport.NewClient` waits for the first handshake so failures are fast and explicit.

### `pkill -f` / `pgrep -f` in verification scripts can kill the calling shell

- **Symptoms**: a smoke script dies with exit 144 or the "restarted" server is still the old one.
- **Fix**: start the server with `& echo $! > pid` and `kill $(cat pid)`; never match on a command
  line that also appears in the script itself.

### Docker-owned files break host operations (`EACCES` / `EPERM` / read-only)

- **Symptoms**: file operations fail with `EACCES`, `EPERM`, "Permission denied" or
  "Read-only file system" on container-generated paths inside the repo.
- **Root cause**: a container wrote to a bind-mounted host directory as root.
- **Agent protocol**: never `sudo`, `chmod -R 777` or loop. Stop and post:

  > ⛔ **Permission denied.** I cannot modify `<path>` while running `<cmd>`. Please run
  > `sudo chown -R $USER:$USER <path>` (or `sudo rm -rf <path>` to discard the artefact), then reply
  > **`continue`** and I will retry from the same step.

  Retry once on `continue`; if the same error repeats, stop and ask the user to confirm the fix.
- **Prevention**: run containers with a matching UID/GID or use named volumes.
