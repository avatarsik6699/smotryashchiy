# CHANGE 07 — Single-Page Dashboard

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `07` |
| Slug | `single-page-dashboard` |
| Title | Single-Page Dashboard |
| Status | `archived` |
| Branch | `feature/07-single-page-dashboard` |

---

## Goal

Stage 3, part 2: the dashboard itself. After login, `/` shows everything on one page separated by
whitespace and hairlines: a STATUS strip, a HOSTS ledger (in-place expandable rows with uPlot
sparklines and live values) and an EVENTS log, plus an "add host" dialog. Data comes from the REST API
and is kept live by the WebSocket stream. No new backend API is expected. See `docs/SPEC.md` §5 (layout,
rules, derived values, chart layout contract) and the approved plan
`/home/niquetamerewsl/.claude/plans/ui-swift-octopus.md`. No alerting, uptime section, tabs, filters,
range switcher or theme switch.

---

## Design References

`docs/reference/DESIGN.md` (tokens/rules, already in `web/src/styles/tokens.css`), `PRODUCT.md`, and
`docs/reference/ui-references/` screenshots 1 and 2 (predecessor dashboard and source detail; their
information is merged into one page here).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Frontend
- [x] `F1` Domain helpers with unit tests: host state by freshness (`OK` ≤ 45 s, `STALE` ≤ 5 min, `OFFLINE`, `NEW`), age/uptime/bytes/rate formatting, counter→rate derivation with reset handling, "latest or `—`" (5 min), disk sparkline mount choice — _Depends on:_ —
- [x] `F2` Data store (`useSyncExternalStore`, no extra libs): initial load (`hosts`, `metrics?latest=true`, `metrics?from=now-1h&step=60` per catalog name, `events?limit=50`, `checks`), 60 s refresh, 5 s clock tick for ages, per-request error/loading state, host view models — _Depends on:_ F1
- [x] `F3` Stream client: `/api/stream` with reconnect backoff, frames update latest values, series tails, events, checks and `last_seen`; connection text `live` / `reconnecting` / `offline`; full refresh after reconnect — _Depends on:_ F2
- [x] `F4` `Chart` and `Sparkline` on uPlot honoring the SPEC §5 layout contract (overflow-hidden/min-width:0 container, ResizeObserver sizing, own legend/tooltip with clamp, fixed axis sizes and short formatters, padding for last tick label, 0–100 for percents, empty/single-point text state, null gaps, theme from CSS variables, rAF-throttled `setData`, destroy on unmount); clamp/format functions unit-tested, uPlot mocked in jsdom — _Depends on:_ F1
- [x] `F5` Command bar (live text, `+ add host`, `logout`) and STATUS strip (hosts, ok/stale/offline/new counts, avg CPU/mem, `—` when unknown) — _Depends on:_ F2, F3
- [x] `F6` HOSTS ledger on Base UI `Accordion` (one open at a time): row = state text + name + meta (uptime, tunnel IP) + four sparklines with values + age; stacks ≤ 900 px; empty/never-seen states — _Depends on:_ F4, F5
- [x] `F7` Expanded host details: large charts (CPU, memory, load 1/5/15, network rx/tx per interface as rates), swap, disks per mount (used/total/percent, Base UI `Meter`), host checks and host events; raw 10 s series fetched on open and extended live; text summary (latest/min/max) for every chart — _Depends on:_ F6
- [x] `F8` EVENTS log (latest 50, newest first, level as text, host name, time) — _Depends on:_ F2, F3
- [x] `F9` Add-host dialog on Base UI `Dialog` + `Field`: name → `POST /api/hosts` → shows the `agent enroll` command, one-time/expiry warning, copy button with `aria-live` confirmation, 409/400/network errors; host list refreshes — _Depends on:_ F2
- [x] `F10` States and errors: loading, empty ("no hosts", add-host hint), load error with retry, per-metric no-data, stale/offline emphasis by text, stream offline notice; keyboard order, focus visible, narrow layout — _Depends on:_ F6, F7, F8, F9
- [x] `F11` Component tests with `vitest-axe` for STATUS, ledger rows, expanded details, events, dialog and each state; store/stream tests with fake fetch and fake WebSocket — _Depends on:_ F10

### Other
- [x] `T1` Docs (STACK.md conventions, KNOWN_GOTCHAS, SPEC touch-ups) and real-browser verification recorded in Implementation Notes: real server plus two real agents; Playwriter at 360/768/1280/1920 px with screenshots reviewed; JS layout-contract check (no element or label outside its chart container, no horizontal page scroll); live update through the stream; server outage and recovery shows `reconnecting` then `live`; add host → run the shown command → the host appears and turns `OK`; keyboard walk-through; console judged in a clean profile — _Depends on:_ F11

---

## Files

### Create / modify
~~~
web/src/domain/            (freshness, formatting, rates)                   [new]
web/src/data/              (store, stream client)                          [new]
web/src/components/        (Chart, Sparkline, StatusStrip, HostRow, HostDetails, Events, AddHostDialog, CommandBar)
web/src/App.tsx, Shell     (compose the single page)
web/src/styles/            (tokens only if a token is missing)
docs/STACK.md, docs/KNOWN_GOTCHAS.md, docs/SPEC.md (touch-ups only)
~~~

### Do NOT touch
- Backend contracts (`docs/SPEC.md` §4.x): a defect found here becomes a Backlog item first
- Agent, transport, rollups/retention, auth internals
- `docs/reference/` (frozen donor)

---

## Contracts

See `docs/SPEC.md` §4.3, §4.6, §4d, §5 (including Derived values and the Chart layout contract).

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: start the server with the built UI, enroll and run a real
agent, open `/` with Playwriter, and confirm within one interval that the host shows `OK` with live
values and sparklines, the layout-contract check passes at 360 and 1280 px, and stopping then
restarting the server shows `reconnecting` then `live` without a page reload.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Impeccable guidance consulted** before UI work (Operate mode: skeletons not spinners, all component states,
  150-250 ms transitions, overlays via portal, no card chrome). The project's own DESIGN.md/PRODUCT.md were the
  pinned brief. The mechanical detector (`detect.mjs`) ran once over `components/` and `styles/`: 0 findings.
- **Deviation from the plan:** host meta shows uptime only. The tunnel IP is not exposed by any API, so it was
  dropped rather than adding an endpoint. Interfaces in the expanded view are capped at 8 (busiest first) with a
  "+ n more" line because container hosts have dozens of idle veth devices. The time axis switches to `HH:MM:SS`
  under 10 minutes of data so ticks do not repeat ("18:02 18:02").
- **Chart layout contract verified in a real browser** with a throwaway lab page of hostile data (huge rates,
  zeros, gaps, one point, 60-180 px containers): all 17 boxes contained, no horizontal scroll at 346/753/1280/1905 px,
  tooltip inside the box at all five edge probes on every chart (80 probes), axis labels fully visible.
  Its first run found the grid-track hazard (see KNOWN_GOTCHAS), now a rule.
- **Real end-to-end (T1)**: real server + real agents vps-a/vps-b (interval 5 s). Playwriter (user's Chrome):
  dark and light x 360/768/1280/1920 px, collapsed and expanded, on the real dashboard: 12/12 charts drawn, no
  overflow or horizontal scroll in all 8 combinations; screenshots reviewed. Live updates through the stream.
  Server restart with the page open: `live -> reconnecting`, login form after 1.0 s (needed a fix, see
  KNOWN_GOTCHAS). Browser<->server link cut with a TCP proxy: `live -> reconnecting -> offline` (banner) ->
  `live` with no reload and no logout. Add host in the UI -> the shown command was executed for real ->
  `vps-c` appeared as `NEW`, then `OK` with live values. Keyboard: Tab order bar -> rows, Enter/Space toggle,
  visible focus ring. Console judged in a clean profile (Playwright MCP, `--disable-extensions`): 0 messages on
  the dashboard incl. the expanded row (uPlot works under `style-src 'self'`).
- Both agents run on the same machine, so their values are equal; differences between hosts are covered by unit
  tests. STALE/OFFLINE rendering is covered by unit tests (`vps-b`, `vps-c` fixtures), not observed live.

---

## Commit Message

```
feat(change-07): single-page dashboard with live hosts and events
```
