# CHANGE 30 — Operator console for the tracked site

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `30` |
| Slug | `operator-console` |
| Title | Operator console for the tracked site |
| Status | `archived` |
| Branch | `feature/30-operator-console` |

---

## Goal

Turn sre.infraege.ru into the operator's console for kodumka.ru (SPEC §4j, §5 «Operator»).
Today kodumka's account-protection signals, manual measures (warn / suspend / terminate / lift),
refunds and the leak-response memo exist only as a CLI on the production host. This change adds:

- a pull-only command channel: the agent asks for queued commands, so the host keeps no open
  ports;
- a closed, host-owned command set;
- an append-only audit log;
- an incident log with 24 h / 72 h RKN countdowns;
- an `operator` view.

The kodumka side — the executor script and the `kodumka.operator/1` and `kodumka.account/1`
documents — is infraegev2 Change 199. The architect decided this on 2026-10-24.

---

## Backlog

### Data
- [x] `D1` Migration `0010_operator.sql`:
  - tables `operator_commands`, `audit_log` and `incidents`, as in SPEC §4j;
  - append-only triggers on `audit_log`: refuse `UPDATE`, and refuse `DELETE` of rows younger than
    3 years;
  - indexes on host and status, and on `at`.
  — _Depends on:_ —

### Backend
- [x] `B1` Domain and application for operator commands:
  - kind allow-list and per-kind args validation, plus the reason;
  - queue, claim (at most 5, marked `running` in one transaction) and result;
  - expiry: `queued` 10 min → `expired`, `running` 5 min → `failed`;
  - clearing results after 30 days and deleting audit rows older than 3 years, both in the existing
    retention job.
  — _Depends on:_ D1
- [x] `B2` Tunnel routes `GET /agent/commands` and `POST /agent/commands/{id}/result`:
  - registered on the tunnel mux (`ServeTunnel`);
  - the host comes from the WireGuard source address, like ingest;
  - body limit 80 KiB; a foreign or non-running command answers `404`.
  — _Depends on:_ B1
- [x] `B3` Operator API:
  - `POST` / `GET /api/operator/commands`, `GET /api/operator/commands/{id}`;
  - step-up admin password for `terminate` and `refund_execute`, counted by the failed-login
    limiter;
  - every write appends an audit row.
  — _Depends on:_ B1
- [x] `B4` Incidents CRUD `/api/operator/incidents` and `GET /api/audit` (paged by `before`). Every
  incident write appends an audit row. — _Depends on:_ D1
- [x] `B5` Agent: `agent run --operator-command PATH`:
  - polls every 15 s, keeps its own kind allow-list;
  - `exec PATH KIND` with the args JSON on stdin: no shell, clean environment, 60 s timeout;
  - stdout must be one JSON object ≤ 64 KiB; the last 2 KiB of stderr go into the error;
  - posts the result; with no flag it never polls.
  — _Depends on:_ B2
- [x] `B6` Go contract tests:
  - tunnel claim and result, and host isolation (a second peer cannot claim or close another's
    command);
  - allow-list refusal in both the agent and the server;
  - args validation, timeout, oversized output;
  - step-up refusal, audit append-only (triggers), expiry and retention;
  - an agent run against a fake executable.
  — _Depends on:_ B3, B4, B5

### Frontend
- [x] `F1` Domain parsers and fixtures: `web/src/domain/operator.ts` for `kodumka.operator/1` and
  `kodumka.account/1`, unknown schema shown as unknown. API client and hooks in `web/src/data/`
  (document every 60 s, command polling until it finishes). — _Depends on:_ B3
- [x] `F2` View `operator`: tab after `sales` in `CommandBar` / `Dashboard`, with these sections:
  - SIGNALS with the confirmation dialog: numbers, reason, password for Terminate;
  - RESTRICTIONS (Lift, Lift + compensate) and REFUNDS (Execute with password);
  - ACCOUNT search → card;
  - INCIDENTS: form, live 24 h / 72 h countdowns for a leak, the response checklist;
  - AUDIT.

  Command state shown as text; help popovers and a guide lesson. Follows `docs/reference/DESIGN.md`
  and `impeccable`.
  — _Depends on:_ F1, B4
- [x] `F3` Vitest:
  - parsers, the dialog (reason required, password for Terminate), countdowns;
  - a11y via vitest-axe.

  Playwriter at 1280 and 390 px.
  — _Depends on:_ F2

### Infra
- [x] `I1` Docs:
  - STACK env and agent flags;
  - RUNBOOK: enabling the operator command on a host and reading the audit;
  - DEPLOY: the agent unit's `--operator-command` and `--document operator=…`;
  - KNOWN_GOTCHAS if anything bites.
  — _Depends on:_ B5

### Other
- [x] `T1` Dependency audit (found by the Full Gate): Go 1.26.10 (GO-2026-6809, `crypto/x509`
  reached from the uptime TLS check) in `go.mod`, the build image and STACK — _Depends on:_ —

---

## Files

### Create / modify
~~~
internal/platform/db/migrations/0010_operator.sql
internal/operator/{domain,application,infrastructure,interfaces/http}/… (new bounded context)
internal/telemetry/interfaces/http/tunnel.go (register the agent command routes)
internal/agent/{runner.go, operator.go (new)}, cmd/smotryashchiy/{agent.go,server.go}
internal/auth/application/service.go (step-up password check reuse)
web/src/domain/operator.ts (+ .fixture.ts, .test.ts), web/src/data/useOperator.ts
web/src/components/Operator/… , web/src/components/Dashboard/{CommandBar,Dashboard}.tsx
web/src/guide/lessons.ts, web/src/api/client.ts (if a PATCH verb is needed)
docs/SPEC.md (§1.3, §4j, §5, §6, §7 — done by /plan), docs/STACK.md, docs/RUNBOOK.md, docs/DEPLOY.md
~~~

### Do NOT touch
- Ingest contract (§4.1) and the existing documents table semantics.
- Analytics and uptime areas.

---

## Contracts

See `docs/SPEC.md` §4j, §5 «Operator» and §6 and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../../STACK.md) — this section only records
> change-specific overrides.

End to end on a dev pair:
1. Run a dev server and an enrolled agent with `--operator-command` pointing at a fake script.
2. Queue Warn from the UI.
3. Expect the command to go `queued → running → done` with the script's JSON result.
4. Expect an audit row for the queueing and for the result.
5. Expect a disallowed kind to be refused with `400` by the server and with `failed` by the agent.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Fast Gate, 2026-10-24.** Lint PASS (gofmt, go vet); `go test ./...` all `ok`; web typecheck
  PASS; 263 vitest tests PASS, axe included; bundle budget 198 507 B of 204 800 B, PASS.
- **Full Gate, 2026-10-24.** fmt/vet, `go mod verify`, `npm ci`, typecheck, web tests with axe,
  build, bundle budget, `go test`, the backup recipe, smoke, Gitleaks and govulncheck — all PASS.
  govulncheck first failed on GO-2026-6809 (`crypto/x509` in Go 1.26.9, reached from the uptime
  TLS check). Go moved to 1.26.10 under `T1`, and the Go rows were rerun.
- **Bundle budget is near its limit (≈3 % left).** Lazy-loading the operator view was tried and
  dropped: `bundle-budget.sh` sums every chunk, so the split added ≈2.6 KB in total instead of
  saving it. The next large UI change needs a budget decision (raise it, or measure only the
  entry chunk).
- **Audit after the action.** A row is written once the command, incident or step-up check has
  run. If the audit write fails, it is logged and the action is not rolled back: the command
  already sits on the host's queue.
- **One tunnel session.** `Sender.Link` is held under a mutex, so ingest and the operator poll
  share one WireGuard session instead of opening two.
- **Unknown schemas.** A report in another schema, or an `account_show` answer in another
  schema, is named as unknown with its schema; it is never parsed as the known one.
- **Browser check (Playwriter, 1280 and 390 px)** on a local server, with an enrolled agent and a
  fake `--operator-command` script:
  - Warn ran `queued → running → done` and showed its JSON result;
  - the account lookup showed the card;
  - an incident was created and marked;
  - the audit log showed every step;
  - the horizontal overflow at 390 px was fixed (`.scroll`, the table's `min-width`, no wrap in the
    action cells).

  A CSP inline-style console error came from Playwriter's screenshot, not from the app. Layout
  guidance came from `impeccable` (shape), keeping the Sales ledger language.
- **Site side.** The site side is infraegev2 Change 199: `scripts/operator-command.sh`,
  `billing.admin operator KIND`, and the `kodumka.operator/1` export timer.

---

## Commit Message

```
feat(change-30): operator console — command queue, audit and incidents
```
