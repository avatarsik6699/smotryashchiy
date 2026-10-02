# CHANGE 25 — Telemetry signal corrections

Status: `archived`.

## Goal

Correct Docker severity misclassification and expose the resource-wait measurements missing
from the 2026-10-02 incident investigation. Existing transport, persistence and authenticated
metric APIs carry the added series. The architect authorized planning and implementing the
audit's necessary code fixes; no release or production configuration change is included.

Branch: `feature/25-telemetry-signal-corrections`.

## Backlog

### Backend

- [x] `B1` Correct the agent's incident signals as one verified set: prefer explicit severity in recognized Docker log formats with conservative stream fallback; add CPU iowait/steal and optional Linux PSI measurements; register the collector and cover classification, valid zeros, missing/malformed data, counter resets and ingest compatibility. — _Depends on:_ —
- [x] `B2` Fix the existing TLS test's calendar-expiring positive certificate fixture discovered by the Fast Gate; derive both valid and expired certificates from the real TLS verification clock without weakening certificate checks. — _Depends on:_ —
- [x] `B3` Address independent review: recognize Zap `DPANIC` as an error so a supported log envelope cannot downgrade it to stream fallback. — _Depends on:_ B1

### Other

- [x] `T1` Update SPEC's additive metric/severity contract and the runbook with authenticated metric queries, source/freshness limits, readiness-target guidance and a bounded disk-stall investigation. — _Depends on:_ B1

## Files

### Create / modify

~~~
internal/agent/collect/collect.go
internal/agent/collect/collect_test.go
internal/agent/collect/pressure.go
internal/agent/collect/pressure_test.go
internal/agent/collect/log_level.go
internal/agent/collect/log_level_test.go
internal/agent/collect/docker_logs.go
internal/agent/collect/docker_logs_test.go
internal/agent/batch_test.go
internal/uptime/application/checker_test.go
cmd/smotryashchiy/agent.go
docs/SPEC.md
docs/RUNBOOK.md
docs/KNOWN_GOTCHAS.md
docs/changes/25-telemetry-signal-corrections.md
~~~

### Do NOT touch

- Auth, schema/migrations, dependency versions, spool durability, SSH error suppression, deployed data.
- Frontend appearance/thresholds: new diagnostic series are available through the existing API;
  this bounded agent fix does not introduce unevidenced alert thresholds or a new dashboard.

## Contracts

See `docs/SPEC.md` §4c and §4h; the existing metric/event envelopes and API routes stay compatible.

## Gate Checks

Use [STACK](../../STACK.md)'s Go Fast Gate for B1, plus focused race checks for modified collectors
and batch validation. T1 is documentation-only. Go LSP is unavailable per STACK; no frontend changes.

Verification (2026-10-02): repository `gofmt` check, `go vet ./...` and
`go test ./... -short` PASS after B2/B3. Focused `go test -race` for agent/collect CPU,
Pressure, DockerLogs, LogLevel and resource-wait batch compatibility PASS; the changed B3
classifier checks were repeated under race after review. `git diff --check` PASS. Independent
read-only review found no remaining blockers after DPANIC coverage. Frontend checks are skipped
(no frontend changes); Go LSP is unavailable per STACK. No release gate was requested.

Release verification (2026-10-02): Full Gate PASS: Go formatting/vet, module verification,
full Go tests/build, frontend frozen install/typecheck/226 tests (21 files)/build, 183528-byte
gzip bundle budget, full-history and working-tree Gitleaks, and govulncheck (no reachable
vulnerabilities). Frontend checks/build were repeated with supported Node 24.21.0 after the
initial local Node patch was found older than the locked jsdom engine requirement. No frontend
source changed, so no additional interactive browser check is required. Release binaries,
image smoke/scan and exact-SHA deployment are the following Release Gate steps.

## Architect Review Notes

- [x] No architect review issues recorded

## Implementation Notes

- A code fix cannot establish or repair the cause of guest-visible disk stalls. The source data
  may stop during a blocked durable spool write; never replace missing measurements with zeros
  or disable fsync as a supposed repair. Existing historical events retain their stored levels.

## Commit Message

```text
fix(change-25): classify log severity and collect resource waits
```
