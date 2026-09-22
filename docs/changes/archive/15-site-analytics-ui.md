# CHANGE 15 — Site Analytics: UI

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `15` |
| Slug | `site-analytics-ui` |
| Title | Site Analytics: UI |
| Status | `archived` |
| Branch | `feature/15-site-analytics-ui` |

---

## Goal

Second half of the v2 "Analytics" backlog item (docs/SPEC.md §4i, §5, §9): the Analytics view —
Monitoring/Analytics tab navigation (the first navigation the project has had), a SITES ledger,
site detail with pageview/visitor stats and top pages/referrers, and the `+ add site` dialog. Pure
frontend against the read API Change 14 already shipped — no backend changes.

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
- [x] `F1` `SiteDTO`/`SiteStatsDTO` wire types (`domain/types.ts`), matching the Change 14 read API
  exactly — _Depends on:_ —
- [x] `F2` A sites data hook (list/create/delete `/api/sites`) — _Depends on:_ F1
- [x] `F3` A site-stats data hook (`GET /api/sites/{id}/stats?range=`), fetched on demand when a
  site row expands or its range toggle changes — _Depends on:_ F1
- [x] `F4` Monitoring/Analytics tab control in the command bar (docs/SPEC.md §5); `+ add host`
  shows only on Monitoring, `+ add site` only on Analytics — _Depends on:_ —
- [x] `F5` `AnalyticsView`: a SITES ledger (Accordion, same pattern as HOSTS) — name, domain,
  today's pageviews/visitors per row — _Depends on:_ F2, F4
- [x] `F6` Site detail (on expand): today/7d/30d range toggle (text buttons), pageviews/visitors
  summary, top 10 pages, top 10 referrers — text ledgers, no charts (deferred per §4i) — _Depends on:_ F3, F5
- [x] `F7` `AddSiteDialog` (name, domain), on creation shows the `<script>` snippet plus the CSP
  note from docs/SPEC.md §5, copy-to-clipboard with `aria-live` — mirrors `AddHostDialog.tsx` —
  _Depends on:_ F2
- [x] `F8` Real browser verification (Playwriter) against the live production deployment: create a
  real site, confirm the tab switch, confirm real stats render (re-run Change 14's beacon
  scenario for real numbers), layout at 360/768/1280/1920 px, no new console errors — _Depends on:_
  F5, F6, F7

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship). -->

---

## Files

### Create / modify
~~~
web/src/App.tsx, components/Dashboard/CommandBar.tsx  (tab state/control)
web/src/components/Analytics/  [new] AnalyticsView.tsx, SiteDetails.tsx, AddSiteDialog.tsx (+ .module.css)
web/src/data/  (sites/site-stats hooks)
web/src/domain/types.ts  (SiteDTO/SiteStatsDTO)
~~~

### Do NOT touch
- Backend/agent code (internal/) — no API changes needed, Change 14 already shipped everything used
- Monitoring view's own components (HostsSection, EventsSection, UptimeSection) beyond the command
  bar's tab control
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §5 (Analytics view layout, tab navigation) and §4i (the read API this consumes).
No new endpoints.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: real verification per F8 against the live production
deployment (`sre.infraege.ru`) with a real site and real beacon data, not a synthetic fixture.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Real, live verification (F8):** deployed to `sre.infraege.ru`, created a real site
  (`infraege.ru`) through the UI dialog, confirmed the tracking snippet and CSP note render, and
  the new row appears in the SITES ledger after creation. Sent real beacons (varied UA/path/
  referrer, matching Change 14's T1 method) and confirmed the UI shows real aggregated numbers:
  5 pageviews, 3 distinct visitors, correctly sorted top pages and top referrers. Range toggle
  (today/7d/30d) confirmed to re-fetch. No new console errors (one pre-existing unrelated Chrome
  extension error). Layout checked at 360/768/1280/1920 px: no horizontal overflow at any width.
  Test site deleted afterward for a clean state.
- Existing keyboard-navigation test (`Dashboard.test.tsx`) updated for the two new tab stops the
  Monitoring/Analytics tabs add to the command bar — not a regression, just a shifted tab order.

---

## Commit Message

```
feat(change-15): dashboard analytics view
```
