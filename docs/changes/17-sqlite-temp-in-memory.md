# CHANGE 17 — Keep SQLite temporary storage in memory

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `17` |
| Slug | `sqlite-temp-in-memory` |
| Title | Keep SQLite temporary storage in memory |
| Status | `active` |
| Branch | `feature/17-sqlite-temp-in-memory` |

---

## Goal

Hourly metric rollups in production have failed since 2026-09-22 11:00 UTC with
`telemetry: rollup hour: disk I/O error (6410)`, and `rollup_state` has not advanced since then.
6410 is `SQLITE_IOERR_GETTEMPPATH`. The container's root filesystem is read-only (SPEC.md §4f), so
SQLite has no writable temp directory. Once an hour's data outgrew SQLite's in-memory buffers, the
rollup's `GROUP BY` / upsert needed a temp file and failed. No data is lost: raw rows are purged only
after their hour is rolled up (§4.5). But long-range charts have a gap from that hour on, and raw
data accumulates past its TTL. This change keeps temporary storage in memory and lets production
catch up.

Reproduced locally before the fix. The current image (`change16`), run `--read-only` against a
synthetic DB holding one hour of 108k metric rows (300 series), logs the same
`disk I/O error (6410)`. The same image and data with a writable rootfs roll up cleanly: 300 rows,
state advanced.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     Group items by area (Backend / Frontend / Infra / Data, etc.).
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. Mark removed items as ~~BN~~ (removed).
     New items always take the next unused ID in their group, appended at the end. -->

### Backend
- [x] `B1` `internal/platform/db/sqlite.go`: add `_pragma=temp_store(MEMORY)` to the DSN. The
      modernc driver runs each `_pragma` on every new connection. — _Depends on:_ —
- [x] `B2` `sqlite_test.go`: a connection from `db.Open` reports `PRAGMA temp_store` = 2 (MEMORY),
      including a connection opened after the pooled one is closed. — _Depends on:_ B1

### Infra
- [x] `I1` Re-run the local reproduction with the fixed image: same synthetic DB, `--read-only`,
      `--cap-drop ALL`, `no-new-privileges`. Expect no 6410 error, 300 rollup rows and
      `rollup_state` advanced. The script lives in the scratchpad, not the repo; the procedure is
      recorded here and in `docs/KNOWN_GOTCHAS.md`. — _Depends on:_ B1

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: a read-only rootfs gives SQLite no temp dir. The failure is
      latent until data grows, and returns `disk I/O error (6410)`. SPEC.md §4f was updated by
      `/plan`. — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/platform/db/sqlite.go
internal/platform/db/sqlite_test.go
docs/KNOWN_GOTCHAS.md
docs/SPEC.md                         (§4f, done by /plan)
~~~

### Do NOT touch
- Rollup/purge SQL and scheduling (`internal/telemetry/...`): the statements are correct. Only
  their temp-file needs were unmet.
- Production compose (`/root/docker-compose.acme.yml`): no `tmpfs` workaround. The fix belongs in
  the app, so every read-only deployment gets it.

---

## Contracts

See `docs/SPEC.md` §4f and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../STACK.md) — this section only records
> change-specific overrides.

```bash
# After deploying to the monitoring VPS: no new 6410 errors, and rollup_state moves past
# 2026-09-22T11:00Z up to the last complete hour (catch-up of the stuck backlog).
docker logs root-server-1 2>&1 | grep -c '6410'
```

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work [XX] review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

None

---

## Commit Message

```
fix(change-17): keep SQLite temporary storage in memory
```
