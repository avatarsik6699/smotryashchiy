# Compacted change history — 01–33

Changes 01–33 have an immutable source snapshot. Current contracts remain SPEC, STACK, DEPLOY and
RUNBOOK; this checkpoint preserves historical decisions, approvals and unresolved risks. The
metadata is verified by `python3 scripts/change_history.py inspect` before history-dependent work
([procedure](../../CHANGE_HISTORY.md)).

<!-- compacted-metadata -->
```json
{
  "version": 2,
  "snapshots": [
    {
      "covered_from": 1,
      "covered_through": 33,
      "source_commit": "647d6dd7ff8d8be15d7891538318bfbbcc419e4e",
      "date": "2026-10-25",
      "missing_numbers": [],
      "source_paths": [
        "docs/changes/archive/01-project-skeleton.md",
        "docs/changes/archive/02-telemetry-contract-and-storage.md",
        "docs/changes/archive/03-rollups-retention-live-stream.md",
        "docs/changes/archive/04-transport-ingest-enrollment.md",
        "docs/changes/archive/05-agent-collectors-run-loop.md",
        "docs/changes/archive/06-ui-foundation.md",
        "docs/changes/archive/07-single-page-dashboard.md",
        "docs/changes/archive/08-uptime-prober.md",
        "docs/changes/archive/09-packaging-and-backup.md",
        "docs/changes/archive/10-acme-deploy-runbook.md",
        "docs/changes/archive/11-docker-logs-fail2ban.md",
        "docs/changes/archive/12-v2-signals-ui.md",
        "docs/changes/archive/13-quiet-healthcheck-noise.md",
        "docs/changes/archive/14-site-analytics-backend.md",
        "docs/changes/archive/15-site-analytics-ui.md",
        "docs/changes/archive/16-track-url-dedupe.md",
        "docs/changes/archive/17-sqlite-temp-in-memory.md",
        "docs/changes/archive/18-collect-origin-check.md",
        "docs/changes/archive/19-audit-hardening.md",
        "docs/changes/archive/20-release-follow-ups.md",
        "docs/changes/archive/21-containers-and-log-levels.md",
        "docs/changes/archive/22-guide-and-health.md",
        "docs/changes/archive/23-routine-ssh-errors.md",
        "docs/changes/archive/24-monitoring-resilience.md",
        "docs/changes/archive/25-telemetry-signal-corrections.md",
        "docs/changes/archive/26-tracker-delivery.md",
        "docs/changes/archive/27-sales-view.md",
        "docs/changes/archive/28-analytics-retention.md",
        "docs/changes/archive/29-backup-health.md",
        "docs/changes/archive/30-operator-console.md",
        "docs/changes/archive/31-backup-failed-units.md",
        "docs/changes/archive/32-code-cleanup.md",
        "docs/changes/archive/33-docs-drift.md"
      ],
      "source_digest": "61f3a5bed8698eadfde2ef52dbd15fcd178ddf625d541ec458188ab595660866"
    }
  ]
}
```

## Snapshot 01–33: outcomes and decisions

Compacted in Change 34 on 2026-10-25 from source `647d6dd7ff8d8be15d7891538318bfbbcc419e4e`.
The 33 archived change files `docs/changes/archive/01-*.md` … `33-*.md` are replaced by this
verified snapshot. Every row below is taken from those files; dates are the ones they record.

| Changes | Outcome and retained decision |
|---|---|
| 01 | One Go binary with `server`/`agent`/`admin`, SQLite (WAL, embedded forward-only migrations), bcrypt admin hash in SQLite set from stdin only, CI with Gitleaks and govulncheck. In-memory sessions: a restart logs the operator out, accepted for a single-user tool. |
| 02–03 | Telemetry contract: zero is a value, >5 min future rejected, replay by key is a no-op, a rejected batch keeps its key; `ON CONFLICT DO NOTHING`, never `INSERT OR IGNORE`. Hourly rollups, 30 d raw / 13 mo rollups; raw rows purged only after their hour is rolled up. WebSocket hub drops slow clients. |
| 04–05 | Userspace WireGuard spike PASS (no root, no kernel module). Single-use hashed enrollment secrets; ingest only on the tunnel, host identity from the `/32` source address. `agent run` never fabricates zeros; the durable spool is written before every send. |
| 06–07 | Embedded SPA (Vite, React, strict TS, Base UI, uPlot) with CSP; everything outside `/api/` is public; `GET /api/auth/session` added. One-page dashboard with a chart layout contract; tunnel IP dropped from host meta (no API exposes it). |
| 08 | Uptime prober (HTTP/TCP/TLS) in its own tables; state derived, never stored; no retries or smoothing; latency kept whenever the target answered. |
| 09–10 | Static amd64/arm64 binaries, distroless nonroot read-only image, streamed backup/restore with manifest hash. ACME via certmagic (`AltHTTPPort` fix); production needs a TLS domain or trusted proxies, never neither. Tag-triggered release workflow and RUNBOOK. |
| 11–13 | Optional Docker, journald, Docker-log and fail2ban collectors; fail2ban via `fail2ban-client` (SPEC updated). Architect approved one EVENTS source filter on 2026-09-22 as a narrow exception. Successful health-check access lines are dropped by the agent. |
| 14–16, 18 | Cookieless JS analytics (architect chose a snippet over log parsing for the SPA), daily-salt visitor hash, collect always `204`. One pageview per URL change; a beacon is stored only from the site's own origin. Architect kept the 72 pre-deploy test rows (2026-09-23). |
| 17 | `temp_store=MEMORY` ended rollup failure `6410` on the read-only rootfs (since 2026-09-22 11:00 UTC); no `tmpfs` workaround. |
| 19–21 | 2026-09-23 audit: cross-origin write guard (`403`, JSON only), HSTS, `/64`-keyed bounded limiters, pathname-only paths, UTC day ranges, deadline-bound Docker stats. Guard now runs before the session check; `pageview_rollups_daily` dropped. UI judges container freshness; server logs Info to stdout. `ufw logging off` on the monitoring VPS 2026-09-23. |
| 22–23 | Architect decisions 2026-09-23: guide tab, `?` help, `normal`/`watch`/`problem` always as text, summary line, English UI, agent `cpu.count`; rules live only in `web/src/domain/health.ts`. Password login stays, so sshd `[preauth]` rejections do not count as errors but remain in EVENTS. |
| 24 | 2026-09-29 overload: architect-reviewed `latest` cache table (raw stays the source of truth), 969 ms → 0.46 ms on synthetic data; certmagic locks kept under `/data/acme`. |
| 25 | Explicit Docker log severity before stream fallback (Zap `DPANIC` is an error); CPU iowait/steal and optional PSI. Missing data is never zero; fsync is not disabled. |
| 26 | `track.js` `data-exclude`, `fetch` keepalive fallback, in-memory collect outcome counters (`GET /api/collect/stats`); Go 1.26.9. |
| 27–28 | Architect decisions 2026-10-19: «Продажи» view from a `business` document carried in the ordinary batch (additive 1.x, migration 0009); production raw TTL 14 d from v0.4.0, analytics TTL separate (default 30). |
| 29, 31 | Architect decision 2026-10-19: BACKUPS ledger from `smotryashchiy.backup/1` documents via `--document KIND=PATH`; off-host Restic recipe (daily, keep last 2, weekly check, monthly drill, nothing on host). Production step I3 moved to the v0.4.0 release checklist; bucket lifecycle `keep-deleted-3d` set 2026-10-19. Change 31 fixed the summary on systemd 255 after the first production run (2026-10-24). |
| 30 | Architect decision 2026-10-24: operator console with a pull-only tunnel command channel, allow-lists on server and agent, step-up password for `terminate`/`refund_execute`, append-only audit (3 years) and incidents with 24 h/72 h RKN countdowns; Go 1.26.10. |
| 32–33 | Architect audit plan 2026-10-25: shared `ratelimit.ClientIP` (enrollment now proxy-aware and `/64`), helpers folded, dead code removed, CI runs the backup recipe row, `restic/restic:0.16.4` pinned by digest; documentation drift corrected, SPEC 68 → 65 KB. |

## Snapshot 01–33: unresolved risks and acceptance limits

- Alerting and notifications were never built (deferred from Change 05 on; SPEC states alerts are
  not built). The architect declined Telegram in Change 29. Health is visible only on the dashboard.
- arm64 was never run on native hardware: Change 09 only cross-compiled it, Change 10 ran the binary
  under QEMU, and image smoke runs on amd64 only. Change 10 checked the release workflow by running
  its steps, not with `act` or a tag push.
- The bundle budget is near its limit (198 507 of 204 800 B after Change 30); the next large UI change
  needs a budget decision.
- Change 24 load results are local synthetic evidence; production observation was a release task.
- Change 25: a code fix cannot establish the cause of guest disk stalls; data may stop during a
  blocked spool write.
- Change 29 I3 was moved to the release checklist. The archive records only the first production
  backup, check and drill runs (Change 31), not the remaining I3 cleanup steps.
- Operator audit rows are written after the action; a failed audit write is logged and the action is
  not rolled back (Change 30).
- Analytics: a site with a CSP must allow this server's origin; the 72 test rows and `/?_=…` paths
  stay; collect counters reset on restart.
- Not observed live, only unit or contract tested: STALE/OFFLINE hosts, a STALE target and an expired
  certificate. `/api/events` silently ignores `from`/`to`.
- Duplicated env and backup descriptions across SPEC, STACK, DEPLOY and RUNBOOK were left on purpose.

## Recovery after Change 34

```bash
python3 scripts/change_history.py read docs/changes/archive/30-operator-console.md
python3 scripts/change_history.py read docs/changes/archive/29-backup-health.md
```

Paths present in several snapshots require `--source <full SHA>`.
