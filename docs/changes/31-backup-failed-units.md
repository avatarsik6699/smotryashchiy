# CHANGE 31 — Backup summary lists failed units on systemd 255

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `31` |
| Slug | `backup-failed-units` |
| Title | Backup summary lists failed units on systemd 255 |
| Status | `archived` |
| Branch | `feature/31-backup-failed-units` |

---

## Goal

Found on the first production run of the off-host backup recipe (Change 29), 2026-10-24. The
backup, check and drill jobs succeeded, but every unit failed: the summary step ran
`systemctl --failed --plain --no-legend 'smotryashchiy-backup@*'`, and systemd 255 (Ubuntu 24.04)
reads the pattern as an unknown verb. Under `set -euo pipefail` the pipeline failed silently, so no
`backup.json` was written and the BACKUPS ledger stayed empty. The contract test missed it
because its fake `systemctl` accepted any arguments.

---

## Backlog

### Infra
- [x] `I1` `deploy/backup/smotryashchiy-backup.sh`: `systemctl list-units --failed …`. — _Depends on:_ —
- [x] `I2` The fake `systemctl` in the contract test refuses a pattern without the `list-units` verb,
  like systemd 255. — _Depends on:_ I1

---

## Gate Checks

Full Gate; the backup recipe row must fail with the old script and the strict fake.

---

## Implementation Notes

- Full Gate 2026-10-24: all rows PASS. The backup recipe row was checked both ways: the old script
  with the strict fake fails, the fixed one passes.
- On the monitoring host the failed runs had still recorded their jobs: backup 12 s, check, and
  drill from the off-site copy. Only the summary was missing.

---

## Commit Message

```
fix(change-31): backup summary lists failed units on systemd 255
```
