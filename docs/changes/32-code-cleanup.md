# CHANGE 32 — Code cleanup: shared client address, duplicates, dead code

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `32` |
| Slug | `code-cleanup` |
| Title | Code cleanup: shared client address, duplicates, dead code |
| Status | `done` |
| Branch | `feature/32-code-cleanup` |

---

## Goal

First block of the architect's audit plan (2026-10-25) for this repository. The changes:
- fix the enrollment rate limit, which keyed clients by the raw TCP peer, so behind a proxy every
  enrollment shared one bucket and IPv6 clients were not grouped by /64;
- fold copied helpers into the platform packages;
- remove dead code;
- align CI with the Full Gate;
- refresh dev-only npm advisories.

---

## Backlog

### Backend
- [x] `B1` `ratelimit.ClientIP(r, trusted)`: one implementation of the trusted-proxy rule, used by
  analytics collect, the login limiter and now enrollment. Enrollment keys by
  `ratelimit.ClientKey`, so IPv6 is grouped by /64; `NewEnrollHandlers` takes the trusted proxy
  list. Unit test for forwarded/untrusted/malformed cases. — _Depends on:_ —
- [x] `B2` `httpserver.TunnelPeer` replaces the copied tunnel-peer parsing in telemetry ingest and
  operator agent routes; `apierror.JSON` replaces four `writeJSON` copies (operator keeps its
  `no-store` wrapper). — _Depends on:_ —
- [x] `B3` Dead code:
  - removed `RKNPrimaryDeadline`/`RKNFinalDeadline` (the web computes deadlines) and the Change-04
    `transport/spike_test.go`, which the contract tests cover;
  - the agent logs a warning when the spool discards unreadable batches on start
    (`Spool.Corrupt`). — _Depends on:_ —

### Web
- [x] `W1` `useDocuments(kind, refreshMs, keep)` backs `useSales` and `useBackups` (same exported
  names and types); `useOperator` keeps its own four-request loader. — _Depends on:_ —
- [x] `W2` Removed unused `LEVEL_LABEL`; guide texts name `kodumka.ru` (the tracked site) instead of
  `infraege.ru`, while `sre.infraege.ru` and host paths stay; `engines.node` `>=24` as `.nvmrc` and
  the Dockerfile. `npm audit fix` updates dev-only vite/vitest/undici/source-map-js (production
  dependencies had 0 advisories). — _Depends on:_ —

### Infra
- [x] `I1` CI runs the backup recipe row (shellcheck + contract test) as the Full Gate requires;
  `restic/restic:0.16.4` pinned by digest in the backup script and the runbook. — _Depends on:_ —

---

## Gate Checks

Full Gate:
- `gofmt` and `go vet`, `go mod verify`, `go test ./...`;
- web: `npm ci`, typecheck, 263 tests, build, bundle budget;
- backup recipe;
- secrets gate, `vuln-gate` and `npm audit`.

### Gate evidence

| HEAD | Groups / focused checks | Result | Seconds |
|------|-------------------------|--------|---------|
| 36a3e39+wt | Full Gate rows above | PASS | 240 |

---

## Implementation Notes

`agent.BuildBatch` stays: it is a documented convenience over `BuildBatchWithDocuments`, and the
tests use it. Test fixtures that name `infraege.ru` as a tracked target stay as data.

---

## Commit Message

```
refactor(change-32): shared client address, fewer copies, dead code, CI backup row
```
