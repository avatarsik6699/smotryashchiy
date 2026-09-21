# CHANGE 02 — Telemetry Contract and Storage

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `02` |
| Slug | `telemetry-contract-and-storage` |
| Title | Telemetry Contract and Storage |
| Status | `active` |
| Branch | `feature/02-telemetry-contract-and-storage` |

---

## Goal

Deliver the trustworthy core of the data plane: validated Metric/Check/Event types, transactional
idempotent ingestion into SQLite and session-protected read endpoints. Encodes the predecessor's
hard-won lessons (zero is a value, producer timestamps are validated, replays are stored once) as
executable contract tests from day one. See `docs/SPEC.md` §4 (Stage 1, part 1 of 2). No HTTP
ingest, enrollment, rollups, retention, WebSocket, UI or alerting.

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

### Data
- [x] `D1` Migration `0002_telemetry.sql`: `hosts`, `metrics`, `checks`, `events`, `ingestion_batches` with the unique record-identity indexes and query indexes from SPEC §4.2 — _Depends on:_ —

### Backend
- [x] `B1` Domain: Metric/Check/Event/Batch value types, validation per SPEC §4.1 (finite value, zero accepted, name/label/status/level/size limits, UTC normalization, >5 min future rejection with an injectable clock) returning field-addressed `apierror.Invalid` errors — _Depends on:_ —
- [x] `B2` Canonical labels serialization (sorted keys, stable JSON) used for identity and storage — _Depends on:_ B1
- [x] `B3` Infrastructure repository: create/get host, transactional batch insert with `INSERT OR IGNORE` counting accepted vs duplicate rows per kind, `ingestion_batches` idempotency record, host `last_seen_at` update — _Depends on:_ D1, B2
- [x] `B4` Application ingest service `Ingest(ctx, hostID, idempotencyKey, batch) (Result, error)`: validates, enforces batch/key limits, unknown host → NotFound, replayed key → `Replayed` no-op, returns accepted/duplicate counts and the accepted records so later alerting can evaluate only new data — _Depends on:_ B1, B3
- [x] `B5` Read side: repository queries and service for metrics (range, limit, `latest`), checks (newest per host+name) and events (newest first, level filter) with bounded limits — _Depends on:_ B3
- [x] `B6` HTTP read handlers `GET /api/metrics`, `/api/checks`, `/api/events` with strict query parsing (RFC 3339 `from`/`to`, `latest` incompatible with range, limit caps) mounted in `server.go` behind `RequireSession` — _Depends on:_ B5
- [x] `B7` Contract tests carrying the KNOWN_GOTCHAS lessons: stored zero metric round-trips through the read API, future timestamp rejected atomically (nothing stored), replay by key and overlap by content, unknown host, empty-vs-stale states distinguishable (no rows ≠ zero) — _Depends on:_ B4, B6

### Other
- [x] `T1` Update `docs/STACK.md` project structure with `internal/telemetry` and add any new gotcha discovered — _Depends on:_ B7

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/platform/db/migrations/0002_telemetry.sql
internal/telemetry/domain/
internal/telemetry/application/
internal/telemetry/infrastructure/
internal/telemetry/interfaces/http/
cmd/smotryashchiy/server.go      (mount read handlers)
docs/STACK.md
docs/KNOWN_GOTCHAS.md            (only if a new pitfall appears)
~~~

### Do NOT touch
- HTTP ingest endpoint, host enrollment, WireGuard, agent code (Stage 2)
- Rollups, retention, WebSocket stream (Change 03), UI, alerting
- `internal/auth`, `docs/SPEC.md` (changed only via `/plan`)

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

Change-specific smoke, after the Full Gate: start the server, log in with the development
password, and confirm the read endpoints answer `200` with empty lists and `401` without a session.

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work 02 review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

<!-- Optional. The agent adds a short bullet here only when something isn't already visible from
     the code or commit history: an intentional deviation from the plan, a residual risk, a
     rejected alternative. Leave empty when nothing needs recording — this is not a mandatory
     per-task log. -->

- Timestamps are stored as unix milliseconds in UTC; producer timestamps are truncated to
  millisecond precision, which is therefore part of record identity.
- Dedup uses `ON CONFLICT DO NOTHING`, not `INSERT OR IGNORE` (which also swallows CHECK
  violations); see `docs/KNOWN_GOTCHAS.md`.
- Unknown JSON fields in a wire batch are ignored so additive 1.x producers do not break an older
  server; the version gate is `schema_version` matching `1.x`.
- A rejected batch does not consume its idempotency key, so the producer can fix and retry it.
- `TestMigrateIsIdempotent` (Change 01) now counts embedded migration files instead of assuming one.

---

## Commit Message

```
feat(change-02): telemetry contract, ingest service, read API
```
