# CHANGE 27 — «Продажи»: business summary from the tracked site

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `27` |
| Slug | `sales-view` |
| Title | «Продажи»: business summary from the tracked site |
| Status | `archived` |
| Branch | `feature/27-sales-view` |

---

## Goal

The architect reads sales and sign-ups of the tracked site (kodumka.ru) on the dashboard instead of
calling a CLI on its server (decision 2026-10-19). The site writes an hourly summary file of counts
and sums (infraege Change 195); the host agent ships it inside its ordinary telemetry batches, so it
rides the existing tunnel, spool and retries, and the server keeps the latest summary per host for a
new «Продажи» view (SPEC §4.1, §5).

---

## Backlog

### Backend
- [x] `B1` Wire batch 1.x additive field `documents: [{kind, body}]` — `kind`
  `^[a-z][a-z0-9_.]{0,63}$`, `body` a JSON object ≤ 512 KiB, ≤ 4 per batch; a batch may carry only
  documents — _Depends on:_ —
- [x] `B2` Migration 0009 `documents (host_id, kind, received_at, body)`, latest per host and kind,
  replaced in the batch's transaction; deleted with its host — _Depends on:_ B1
- [x] `B3` `GET /api/documents?kind=K` (session) → `{"documents":[{host_id, host_name, kind,
  received_at, body}]}` — _Depends on:_ B2
- [x] `B4` Agent `run --business-file PATH`: when the file's content changes (and once at start),
  the next batch carries it as kind `business`; a missing file is quiet, an unreadable or invalid
  one is logged once — _Depends on:_ B1

### Frontend
- [x] `F1` Tab «Продажи»: freshness of the summary; KPI tiles (revenue and paid orders for 30 days,
  conversion orders → paid, active access, ending in 7 days); revenue and orders/paid by day charts;
  day × plan table; registrations by day with registration → payment conversion; НПД limit bar;
  problems (stuck orders, refunds, last reconciliation) shown as warnings only when non-zero; an
  empty state when no summary has arrived — _Depends on:_ B3

### Other
- [x] `T1` SPEC §4.1/§5, DEPLOY (agent flag), RUNBOOK — _Depends on:_ B4, F1

---

## Files

### Create / modify
~~~
internal/telemetry/domain/*, internal/telemetry/application/*, internal/telemetry/infrastructure/*
internal/telemetry/interfaces/http/*, internal/platform/db/migrations/0009_documents.sql
internal/agent/*, cmd/smotryashchiy/agent.go
web/src/components/Sales/*, web/src/components/Dashboard/Dashboard.tsx, web/src/api|domain
docs/SPEC.md, docs/DEPLOY.md, docs/RUNBOOK.md
~~~

### Do NOT touch
- Analytics (§4i), uptime, auth; `docs/changes/archive/`

---

## Contracts

See `docs/SPEC.md` §4.1, §5 and the Files list above. The summary schema is
`kodumka.business/1` (infraege `app/modules/billing/admin.py export`).

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../../STACK.md) — this section only records
> change-specific overrides.

Browser check of «Продажи» with Playwriter at 1280 and 390 px against a server fed with a sample
summary.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- The summary rides the ordinary telemetry batch (`documents`, additive 1.x) instead of a new
  endpoint or uplink method: it reuses the tunnel, the durable spool and its retries.
- Daily figures are a ledger with inline revenue bars, not uPlot charts: the chart's time axis is
  clock-based and would mislabel days.
- The view is English like the rest of the dashboard; plan names come from the summary.
- Browser check (Playwriter, own Chrome): local server fed by `agent push-file` with a 60-day sample;
  1280 px and 390 px (no page overflow; the day table scrolls inside). Impeccable detector: clean.

---

## Commit Message

```
feat(change-27): «Продажи» view fed by host business summaries
```
