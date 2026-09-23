# CHANGE 22 — Guide tab, "?" help, health assessment and a summary line

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `22` |
| Slug | `guide-and-health` |
| Title | Guide tab, "?" help, health assessment and a summary line |
| Status | `archived` |
| Branch | `feature/22-guide-and-health` |

---

## Goal

The architect finds the dashboard hard to read. It shows many numbers but explains neither what
they mean nor what normal looks like. Architect decision on 2026-09-23; all text stays in
English:
- a `guide` tab with a step-by-step course, showing live values from the operator's own hosts;
- a `?` popover on every block;
- `normal` / `watch` / `problem` assessment against fixed thresholds, always written as text too;
- a summary line at the top of Monitoring.

Load average needs the CPU count, which no metric carried, so the agent gains `cpu.count`.
Contract: `docs/SPEC.md` v1.22 (§4c, §5).

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
- [x] `B1` The agent reports `cpu.count` through a new `CPUCount` collector, counting the `cpuN`
      lines of `/proc/stat` on every tick. It never reports 0: no per-core lines is an error.
      Written test first. — _Depends on:_ —

### Frontend
- [x] `F1` `web/src/domain/health.ts`: one function per signal that returns
      `{level: normal|watch|problem|unknown, reason}`, following the SPEC §5 threshold table.
      Table tests cover every boundary and every unknown input. — _Depends on:_ —
- [x] `F2` Error events of the last hour:
      - the store loads `/api/events?level=error&limit=500` with the full load;
      - live `error` frames extend it, trimmed to one hour;
      - a store/model test covers both.

      — _Depends on:_ —
- [x] `F3` `fleetSummary` (in `web/src/data/health.ts`, with `hostHealth`/`targetHealth`) and a summary line above STATUS: worst level first, then
      the facts (hosts reporting, targets up and their latency, the fullest disk, errors in the
      last hour); `role="status"`. Tests cover all good, watch, problem and no data.
      — _Depends on:_ F1, F2
- [x] `F4` Assessment colors plus a text marker (`watch` / `problem`) on:
      - HostRow CPU, MEM and DISK, and the host state;
      - the HostDetails disks, containers and checks, plus a new swap/load line;
      - UPTIME rows and the STATUS strip.

      Use the existing `--status-*` tokens, in both themes. Tests plus axe. — _Depends on:_ F1
- [x] `F5` The `HelpButton` popover (base-ui Popover; check the API with Context7) on every
      section and block title, with the explanation, the thresholds and a "read the lesson"
      button that opens the guide at that lesson. Tests for keyboard use and axe.
      — _Depends on:_ F1, F6
- [x] `F6` The `guide` tab:
      - lessons as typed data in `web/src/guide/lessons.ts` (14 lessons from the plan);
      - a table of contents and "lesson N of M" navigation;
      - live callouts from the dashboard state and sites, judged by `health.ts`;
      - lazy-loaded to keep the bundle budget;
      - the last opened lesson remembered in `localStorage`, wrapped in try/catch.

      Tests: the tab, every lesson renders, live values appear, anchor navigation works, axe.
      — _Depends on:_ F1

### Infra
- [x] `I1` RUNBOOK: the v0.2.6 agent update. Updating both agents is a release-time Gate Check.
      — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate and opt-in Full Gate.
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/agent/collect/collect.go (+ test), cmd/smotryashchiy/agent.go            (B1)
web/src/domain/health.ts, web/src/data/health.ts (+ tests)                         (F1, F3)
web/src/data/model.ts, web/src/data/store.ts (+ tests)                             (F2)
web/src/components/Dashboard/*.tsx (+ css, tests)                                  (F3, F4, F5)
web/src/components/Analytics/{AnalyticsView,SiteDetails}.tsx                       (F5)
web/src/components/{Help,Health,Guide}/*, web/src/guide/{lessons,live}.ts          (F4-F6)
docs/SPEC.md (v1.22, done by /plan), docs/RUNBOOK.md                               (I1)
~~~

### Do NOT touch
- Server API and storage (the existing `level` filter on `/api/events` is enough).
- Alerting (still deferred); the Analytics assessment (explained only).

---

## Contracts

See `docs/SPEC.md` §4c and §5 and the Files list above. Do not hand-copy the schema, endpoints,
types, or env vars into this file — the codebase and `SPEC.md` are the source of truth; this file
only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs on every `/work`; Full Gate runs only on `/ship`. All gates are defined in
> [docs/STACK.md](../../STACK.md) — this section only records change-specific overrides.

Local check before ship, with Playwriter against a dev server fed through `agent push-file`
(production shape plus edge values: disk 85 %, a stale host, error events):
- the summary line at each level;
- colors together with their text;
- `?` popovers opened by keyboard and mouse, and the lesson link;
- the guide with its live callouts;
- 360 px width with no horizontal scroll, both themes, and no console errors.

After release: back up and upgrade the server to v0.2.6, then update both agents. On production,
check that `cpu.count` arrives, load per CPU is assessed, the summary line matches reality, and
the guide shows live values for both hosts.

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

- Assessment rules live only in `web/src/domain/health.ts`. Everything else reads from there:
  - `web/src/data/health.ts` composes them per host and target and builds `fleetSummary`;
  - the dashboard colors, the `?` popovers, the summary line and the guide's live callouts.
- Error count: the server's `level` filter matches exactly, so the store loads both `error` and
  `critical`, filtering on the client as well.
- Offline hosts are judged by freshness alone. Stale hosts keep their assessed values, because
  those are still under 5 minutes old.
- Help buttons sit next to titles, not inside them, so heading names stay the plain titles.
  Charts in the expanded host gained titles (CPU, Memory and swap, Load, Network) to carry them.
- The guide chunk is lazy-loaded (2.5 KB gzip); lesson summaries ship in the main bundle for the
  popovers. Main bundle 183 KB of the 200 KB budget.
- Local Playwriter check against a seeded dev server, with production-shaped hosts, disk at 85 %,
  error and critical events, and hosts turning STALE. Verified:
  - the summary line at the watch level, and the colors with their text;
  - the popover by keyboard, with Escape returning focus, and the lesson link;
  - the guide with live values;
  - 360 px width with no horizontal scroll, and the light theme;
  - no CSP violations raised by the page.

  Found and fixed during the check: the error count appeared twice in the summary, and the disk
  meter kept its accent color at the watch level.

---

## Commit Message

```
feat(change-22): guide tab, help popovers, health assessment, summary
```
