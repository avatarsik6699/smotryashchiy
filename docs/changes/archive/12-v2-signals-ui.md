# CHANGE 12 — v2 Signals UI

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `12` |
| Slug | `v2-signals-ui` |
| Title | v2 Signals UI |
| Status | `archived` |
| Branch | `feature/12-v2-signals-ui` |

---

## Goal

First v2-backlog item (docs/SPEC.md §9): a dedicated UI for the signals Change 11 added
agent-side (Docker container metrics, journald/Docker-log events, fail2ban checks), which today
only reach the dashboard through the generic Checks/Events sections with no source visibility. Pure
frontend — the read API already returns everything needed (`labels` on events, `meta` on checks).

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

### Frontend
- [x] `F1` CONTAINERS block in the expanded host row (`HostDetails.tsx`): group
  `docker.container.*` metrics by the `container` label (name, image, current CPU%, current
  memory used/percent), busiest-first sort like INTERFACES, block omitted when a host has no such
  metrics — _Depends on:_ —
- [x] `F2` CHECKS block: render a non-empty `Check.meta`'s key/value pairs inline next to status,
  generically (no fail2ban-specific branching) — _Depends on:_ —
- [x] `F3` EVENTS: show each row's source label (`unit`/`container`/`jail`, whichever is present)
  in both the dashboard-level `EventsSection.tsx` and the host-detail events list in
  `HostDetails.tsx` — _Depends on:_ —
- [x] `F4` EVENTS: one source filter (Base UI control), client-side against the already-loaded
  window, no new API call — _Depends on:_ F3
- [x] `F5` Real browser verification (Playwriter) against the live production deployment
  (`sre.infraege.ru`): containers panel, check meta, event labels + filter, chart/layout contract
  at 360/768/1280/1920 px, no new console errors — _Depends on:_ F1, F2, F3, F4

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship). -->

---

## Files

### Create / modify
~~~
web/src/components/Dashboard/HostDetails.tsx, HostDetails.module.css
web/src/components/Dashboard/EventsSection.tsx, EventsSection.module.css
web/src/data/ (selectors/grouping helpers as needed, e.g. detail.ts)
docs/SPEC.md §5 (already drafted)
~~~

### Do NOT touch
- Backend/agent code (internal/agent/, internal/telemetry/) — no API or wire changes needed
- ACME/release/runbook (Change 10), uptime prober (Change 08)
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §5 (CONTAINERS panel, generic Check metadata, EVENTS source label + filter) and
§4.3/§4h for the existing `labels`/`meta` fields this reads. No new endpoints or query params.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: verify in a real browser against the live
`sre.infraege.ru` deployment (already monitoring real production data with all three Change-11
signal types) rather than synthetic fixtures — confirms the new UI renders real container/event/
check data correctly, not just its own mocked unit tests.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Spec-level deviation approved by the architect during /plan (2026-09-22):** the original
  "no filters" UI rule (docs/SPEC.md §5) blocked F4 as originally briefed. The architect decided to
  remove that blanket restriction rather than work around it; §5 now documents EVENTS' one source
  filter as an explicit, narrow exception (tabs/theme-switch/other-section filters remain out).
- **F5 verification gotcha:** the already-open browser tab kept serving the pre-deploy
  `index-BkxKNJg0.js` bundle after the new image was live (`assets/*` is served `immutable`,
  docs/SPEC.md §4d) — a plain reload wasn't enough to prove anything until a fresh `curl` against
  the server confirmed the new asset hash (`index-CH5DuPyK.js`) was actually being served, and a
  full page reload picked it up. A stale open tab, not a deploy failure — worth remembering for the
  next live-browser check.
- **Real, live verification (F5):** confirmed against `sre.infraege.ru`'s real production data —
  CONTAINERS panel showing all 9 real infraege containers with live CPU/memory, CHECKS showing both
  fail2ban jails' `currently_banned` inline (sshd=5/6 during the session, real attack traffic),
  EVENTS showing real source labels across the board (`infraege-api-1`, `infraege-nginx-1`,
  `docker.service`, `ssh.service`, `cron.service`) and the source filter interactively narrowing to
  exactly one source's events. Layout checked at 360/768/1280/1920 px: no horizontal overflow, no
  clipped text. No new console/page errors (one pre-existing unrelated error from a Chrome
  extension, not this app).

---

## Commit Message

```
feat(change-12): dashboard UI for Docker/log/fail2ban signals
```
