# CHANGE 11 — Docker, Logs and fail2ban Collectors

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `11` |
| Slug | `docker-logs-fail2ban` |
| Title | Docker, Logs and fail2ban Collectors |
| Status | `archived` |
| Branch | `feature/11-docker-logs-fail2ban` |

---

## Goal

Stage 5 (docs/SPEC.md §7, §4h): give the agent three new read-only, independently-optional
collectors — Docker container metrics, journald + Docker container log tailing forwarded as
events, and fail2ban ban/unban events plus per-jail status checks. No control actions, no new wire
format (the existing `metrics`/`checks`/`events` batch shape already covers all three), no dashboard
surfacing yet.

---

## Design References

<!-- none provided -->

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Data
- [x] `D1` Extend `agent.BuildBatch` (or a sibling) to carry `checks` and `events` alongside
  `metrics` in one wire batch (§4.1) — today it only accepts `collect.Sample` — _Depends on:_ —

### Backend
- [x] `B1` Producer interfaces for check/event sources alongside the existing `collect.Collector`
  (metric samples only today) — _Depends on:_ D1
- [x] `B2` Docker container metrics collector: local socket (`unix:///var/run/docker.sock`),
  read-only API calls, `docker.container.cpu_percent`/`memory_used_bytes`/`memory_used_percent`
  labeled `container`/`image`; disables itself (logged once) if the socket is absent/unreadable —
  _Depends on:_ B1
- [x] `B3` journald log collector: `journalctl -f -o json` tail forwarded as `events`
  (level from syslog priority, `unit` label); disables itself on non-systemd hosts — _Depends on:_ B1
- [x] `B4` Docker container log collector: tail via the Docker API, forwarded as `events`
  (`container` label) — _Depends on:_ B1
- [x] `B5` fail2ban collector: ban/unban lines tailed from its log file forwarded as `events`
  (`jail` label), per-jail currently-banned count as `checks` rows (`name=fail2ban.jail.<jail>`,
  `meta.currently_banned`) sourced from `fail2ban-client status`/`status <jail>` — _Depends on:_ B1
- [x] `B6` Wire B2–B5 into `agent run`'s loop (`cmd/smotryashchiy/agent.go`); each collector's
  failure/absence is independent and never stops the others — _Depends on:_ B2, B3, B4, B5

### Infra
- [x] `I1` `docs/STACK.md` / `docs/KNOWN_GOTCHAS.md` updates for any new gotchas found (Docker
  socket permissions, journald availability, fail2ban log path/format); Context7 lookup before
  writing code against the Docker HTTP API — _Depends on:_ B6

### Other
- [x] `T1` Real verification on the Stage-7 target VPS (2.26.8.245: already running Docker
  containers, and infraege's own security stack — fail2ban is active there per the Stage 7
  recon), plus `docs/SPEC.md` cross-links — _Depends on:_ B6, I1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/agent/collect/            (new collector types: docker, journald, fail2ban)
internal/agent/agent.go, batch.go  (BuildBatch: checks + events alongside metrics)
cmd/smotryashchiy/agent.go         (wire new collectors into the run loop)
docs/SPEC.md §4h (already drafted), docs/STACK.md, docs/KNOWN_GOTCHAS.md
~~~

### Do NOT touch
- Server-side ingest/storage/API (§4.1–§4.3 already support these shapes; no schema change)
- UI, uptime prober, ACME/release/runbook (Changes 06–10)
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4h (and §4.1, §4c for the existing wire/collector shapes) and the Files list
above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](./STACK.md) — this section only records
> change-specific overrides.

Change-specific smoke, after the Full Gate: deploy the updated agent binary to the Stage-7 target
VPS (already enrolled, already running Docker + fail2ban) and confirm via `/api/metrics` /
`/api/events` / `/api/checks` that container metrics, at least one real log line, and fail2ban jail
status all arrive — real infrastructure, not mocked collectors.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **fail2ban jail names vs. the name regex (found via T1's real deployment, not local testing):**
  the server's metric/check `name` field forbids hyphens, but fail2ban jail names routinely have
  them (`infraege-nginx-limit`). Fixed by sanitizing the jail name into the check name
  (`checkNameSafe`) while keeping the original in `Meta.jail`. See `docs/KNOWN_GOTCHAS.md`.
- **B5 design deviation from the original plan:** the plan avoided a `fail2ban-client` subprocess
  in favor of parsing fail2ban's own jail-config cascade (`jail.conf`/`jail.d/*`/`jail.local`).
  During implementation this was revised to use `fail2ban-client status`/`status <jail>` instead —
  the config cascade's include/override semantics are materially more complex to parse correctly,
  while `fail2ban-client`'s text output is stable and well-documented, and the agent already runs
  as root in our own deployment so the extra privilege is not a new cost. `docs/SPEC.md` §4h
  updated to match before implementing.
- **Real, live verification (T1):** deployed the rebuilt agent to the Stage-7 target VPS
  (2.26.8.245, infraege production — already running Docker with 9 containers, journald, and an
  active fail2ban with real banned IPs from real SSH brute-force attempts). Confirmed via
  `/api/checks`, `/api/events`, `/api/metrics` that all three new signal types arrived with real
  data: per-container `docker.container.cpu_percent` for all 9 running containers, journald events
  (systemd units, SSH session opens, a live UFW block line) and Docker container log events
  (`infraege-api-1`'s own structured request logs), and fail2ban per-jail checks (`sshd`:
  `currently_banned=5`, matching real attack traffic seen in the same event stream).
- Timestamps: a tailed collector's events (journald, fail2ban ban/unban) carry their own `TS`
  rather than the batch's tick time, to avoid the server's `(host, ts, level, message, labels)`
  event identity silently collapsing two distinct same-tick lines into one row; see
  `docs/KNOWN_GOTCHAS.md`.

---

## Commit Message

```
feat(change-11): agent Docker, log and fail2ban collectors
```
