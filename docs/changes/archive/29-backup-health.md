# CHANGE 29 — Backups: health ledger and off-host backup recipe

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `29` |
| Slug | `backup-health` |
| Title | Backups: health ledger and off-host backup recipe |
| Status | `archived` |
| Branch | `feature/29-backup-health` |

---

## Goal

The architect wants to see on the dashboard, early and without notifications, that backups exist
and work. That covers the tracked site (kodumka.ru, infraege Change 196) and this monitoring server
itself (decision 2026-10-19). A host that runs backups writes a `smotryashchiy.backup/1` summary.
Its agent ships it as a `backup` document, and a new **BACKUPS** ledger on the Monitoring view
assesses it: missing, stale or failed jobs, holds past review, low disk and failed units.

The monitoring server's own database gets an off-host recipe:
- a daily encrypted Restic copy to a remote repository (S3 in production), keeping only the last 2
  snapshots and nothing on the host, because monitoring data is not critical;
- a weekly check and a monthly restore drill.

Ships in v0.4.0 with Changes 27–28 at the general release (SPEC v1.24: §1.3, §4.1, §4f, §5).

---

## Backlog

### Backend
- [x] `B1` Agent `run --document KIND=PATH`:
  - repeatable, ≤ 4 kinds, unique, kind regex as §4.1;
  - same change detection and quiet-missing behaviour as `--business-file`, which becomes an alias
    of `--document business=PATH`;
  - usage text and CLI tests.
  - — _Depends on:_ —

### Frontend
- [x] `F1` `web/src/domain/backup.ts` + fixture:
  - parse the `smotryashchiy.backup/1` schema;
  - assess per §5 thresholds (job failed / stale / never / not-configured, hold past review, disk
    free < 15 % / < 5 %, failed units, summary > 2 h);
  - worst level per host and overall;
  - unit tests;
  - another schema → unknown.
  - — _Depends on:_ —
- [x] `F2` `data/useBackups.ts`: loads `/api/documents?kind=backup` with the dashboard's 60 s
  refresh. — _Depends on:_ F1
- [x] `F3` **BACKUPS** ledger in the Monitoring view between UPTIME and EVENTS:
  - same hairline ledger and Accordion pattern as HOSTS;
  - a row shows the host, the assessment in text, the newest job age, holds and backup-disk free;
  - expanding it shows every job (label, status, age vs limit, duration, source), repositories
    (snapshots, size) and failed units;
  - the empty state explains `--document backup=PATH`.
  - Design per `impeccable`, matching the existing ledgers; the UI stays English.
  - — _Depends on:_ F1, F2
- [x] `F4` Backups' worst level joins the summary line and the STATUS strip, with the same
  text-first assessment. — _Depends on:_ F1, F3
- [x] `F5` Help: `?` popover on BACKUPS with the thresholds, and guide lesson 15 «Backups» showing
  the operator's live backup rows. — _Depends on:_ F3

### Infra
- [x] `I1` `deploy/backup/smotryashchiy-backup.sh` + `smotryashchiy-backup.{service,timer}`:
  - daily: `admin backup --out -` → `restic backup --stdin` (pinned `restic/restic` image) →
    `forget --keep-last 2 --prune`, nothing kept on the host;
  - `check` mode, weekly: `restic check --read-data-subset`;
  - `drill` mode, monthly: restore the latest bundle and run `admin restore` into a throwaway volume.
  - One root-only env file holds `RESTIC_REPOSITORY`, `RESTIC_PASSWORD` and the backend keys; the
    script refuses a wrong owner or mode.
  - Each mode rewrites `/var/lib/smotryashchiy/backup.json` (`smotryashchiy.backup/1`) atomically;
    a failure records `failed` and exits non-zero.
  - Shell tests with fake docker and restic.
  - — _Depends on:_ —
- [x] `I2` Docs:
  - RUNBOOK «Backup schedule» replaces the local cron with this recipe, plus restore from the remote
    repository on a fresh host;
  - DEPLOY backup section;
  - STACK lists the new tests;
  - the agent flag in §4.1 docs.
  - — _Depends on:_ B1, I1
- [x] ~~`I3`~~ (moved to the general release checklist with v0.4.0, architect 2026-10-19) Production at the v0.4.0 release, with the architect's permission:
  - **Monitoring host:**
    - env file from athanor: `SMOTRYASHCHIY_BACKUP_S3_*` from the smotryashchiy vault,
      `RESTIC_PASSWORD` from the infraegev2 vault;
    - repository `s3:https://storage.yandexcloud.net/kodumka-backup-monitoring`;
    - install the timers and run the first backup and drill;
    - delete `/root/smotryashchiy-backup.cron.disabled` and any `/var/backups/smotryashchiy`
      leftovers;
    - its agent gets `--document backup=/var/lib/smotryashchiy/backup.json`.
  - **infraege-prod agent:** `--document business=…` and `--document backup=/var/lib/infraege/ops/backups.json`.
  - — _Depends on:_ I2
- [x] `I4` Bucket `kodumka-backup-monitoring`: lifecycle noncurrent-version expiration 30 d → 3 d
  (outside the repo, Yandex Cloud console). — _Depends on:_ —

---

## Files

### Create / modify
~~~
cmd/smotryashchiy/agent.go (+ tests)
internal/agent/** (document collectors, if the flag parsing lives there)
web/src/domain/backup.ts, backup.fixture.ts, backup.test.ts
web/src/data/useBackups.ts
web/src/components/Backups/** (ledger + module CSS + tests)
web/src/components/Dashboard/** (placement, summary line, STATUS strip)
web/src/components/Help/**, web/src/components/Guide/** (popover, lesson 15)
deploy/backup/smotryashchiy-backup.sh, smotryashchiy-backup.service, smotryashchiy-backup.timer
scripts/tests/ or deploy/backup/tests/ (shell tests)
docs/SPEC.md (v1.24 — done by /plan), docs/RUNBOOK.md, docs/DEPLOY.md, docs/STACK.md
~~~

### Do NOT touch
- Wire batch schema, migrations and `GET /api/documents`: documents (Change 27) already carry any kind.
- Server-side scheduling or S3 code in the binary: the backup stays an operator recipe (§1.3).
- Alerting and notifications (§7, deferred; the architect declined Telegram here).
- The Sales view (Change 27) beyond reusing shared pieces.

---

## Contracts

See `docs/SPEC.md` §4.1 (documents, `smotryashchiy.backup/1`), §4f (off-host recipe), §5 (BACKUPS
ledger, thresholds) and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../../STACK.md) — this section only records
> change-specific overrides.

UI verified with Playwriter at 1280 and 390 px with fixture documents (fresh, stale, failed,
not-configured, hold past review).

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- The ledger row shows the summary's **first job** as the host's primary backup (SPEC §4.1), not
  the newest success of any job: a passing weekly check must not hide a failing nightly backup.
- `restic backup --stdin` keeps a snapshot even when `admin backup` failed. With `--keep-last 2`
  that would push a good copy out, so the script forgets that snapshot (KNOWN_GOTCHAS). The
  contract test was mutation-checked against this.
- I4 done 2026-10-19 in the Yandex Cloud console: `kodumka-backup-monitoring` lifecycle rule
  `keep-deleted-3d`.
- Fast Gate 2026-10-19: Go lint and unit tests, web typecheck, 253 web tests (axe included), backup
  recipe shellcheck and contract test, all PASS. Playwriter: BACKUPS at 1280 and 390 px (expanded
  row; no page overflow, the job table scrolls inside). Impeccable detector: clean.

---

## Commit Message

```
feat(change-29): backup health ledger and off-host backup recipe
```
