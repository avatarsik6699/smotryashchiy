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
