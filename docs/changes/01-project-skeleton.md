# CHANGE 01 — Project Skeleton

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `01` |
| Slug | `project-skeleton` |
| Title | Project Skeleton |
| Status | `active` |
| Branch | `feature/01-project-skeleton` |

---

## Goal

Deliver the runnable foundation of `smotryashchiy`: one Go binary with `server` / `agent` / `admin`
modes, typed configuration, SQLite with embedded migrations, liveness/readiness endpoints and
single-admin-password authentication, all guarded by CI, a secrets scan and a vulnerability audit.
No UI, agent logic or ingestion yet. See `docs/SPEC.md` §3 and §7 Stage 0.

---

## Design References

<!-- Optional. Populated by /plan when design assets (Figma, mockups, screenshots) are provided.
     Remove this section entirely if no design assets exist for this change.
     Format: `Screen name — brief description (key components, interactions)` -->

<!-- none provided -->

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
- [x] `B1` Entrypoint `cmd/smotryashchiy` with `server`, `agent` (stub that exits with a clear "not implemented" message) and `admin` subcommands; typed env config with validation and a build-time release SHA — _Depends on:_ —
- [x] `B2` SQLite (pure-Go driver, WAL) with embedded forward-only migrations and a `settings`/`admin` table for the bcrypt hash — _Depends on:_ B1
- [x] `B3` HTTP server with graceful shutdown, `GET /healthz` and `GET /health/ready` (`{status, release}`; checks DB), request logging and uniform JSON errors — _Depends on:_ B1, B2
- [x] `B4` Auth service ported from sre-kit `internal/auth`: bcrypt verify, in-memory sessions, `POST /api/auth/login`, logout, session middleware, `HttpOnly`/`SameSite=Lax` cookie (`Secure` in production), per-client login rate limit — _Depends on:_ B2, B3
- [x] `B5` `admin set-password` reads the password from stdin only (never argv/env/logs) and stores the bcrypt hash; production refuses to start without a preseeded hash — _Depends on:_ B2
- [x] `B6` Unit and integration tests: config validation, migrations idempotence, readiness with a broken DB, login success/failure/rate-limit, session expiry, cookie attributes, stdin-only password path — _Depends on:_ B3, B4, B5

### Infra
- [x] `I1` GitHub Actions `ci.yml`: gofmt, vet, test, build, `go mod verify`, pinned action SHAs, read-only token — _Depends on:_ B1
- [x] `I2` Secrets scan script (gitleaks, pinned version, full history) and `govulncheck` step in CI — _Depends on:_ I1

### Other
- [x] `T1` Update `docs/STACK.md` Fast/Full/Release gate rows (secrets scan, dependency audit, smoke) and add a short setup section — _Depends on:_ I2
- [x] `T2` Add `.env.example` documenting every config variable — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
cmd/smotryashchiy/main.go
internal/platform/config/
internal/platform/db/            (sqlite.go, migrations/*.sql)
internal/platform/httpserver/
internal/auth/                   (domain, application, interfaces/http)
.github/workflows/ci.yml
scripts/secrets-gate.sh
.env.example
docs/STACK.md
docs/changes/01-project-skeleton.md
go.mod, go.sum
~~~

### Do NOT touch
- Any frontend, agent collection logic, ingest/telemetry storage, WireGuard, uptime or alerting code
- `docs/SPEC.md` (approved; spec-level changes go through a separate `/plan`)
- `docs/reference/` (design donors, read-only)

---

## Contracts

See `docs/SPEC.md` §3–§4 (and §5–§7 where relevant) and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](./STACK.md) — this section only records
> change-specific overrides.

Change-specific smoke, run after the Full Gate:

```bash
SMOTRYASHCHIY_ADDR=127.0.0.1:18080 go run ./cmd/smotryashchiy server &
curl -fs http://127.0.0.1:18080/healthz
curl -fs http://127.0.0.1:18080/health/ready   # expect {"status":"ok","release":...}
```

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work 01 review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

<!-- Optional. The agent adds a short bullet here only when something isn't already visible from
     the code or commit history: an intentional deviation from the plan, a residual risk, a
     rejected alternative. Leave empty when nothing needs recording — this is not a mandatory
     per-task log. -->

- Deviation from the predecessor: the admin bcrypt hash lives in SQLite instead of an encrypted
  secrets file (a hash is not reversible and the new product stores no third-party credentials).
  Sessions stay in memory, so a restart logs the operator out — accepted for a single-user tool.
- `go.mod` pins Go 1.26.6: govulncheck flagged fixed stdlib CVEs (GO-2026-6089/6090/5972) in 1.26.5,
  and CI takes its toolchain from `go.mod`.
- `.gitleaks.toml` allowlists only the fake test password (`correct-horse-battery-staple`) in
  `_test.go` files; the `generic-api-key` rule stays enabled everywhere else.
- Login also gained `POST /api/auth/logout` (not in the original Backlog wording); it is part of the
  session lifecycle and needs no schema or contract change.

---

## Commit Message

```
feat(change-01): project skeleton — modes, SQLite, auth, CI
```
