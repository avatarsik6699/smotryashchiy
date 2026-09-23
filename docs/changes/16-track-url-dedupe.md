# CHANGE 16 — Count a pageview per URL change, not per history call

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `16` |
| Slug | `track-url-dedupe` |
| Title | Count a pageview per URL change, not per history call |
| Status | `active` |
| Branch | `feature/16-track-url-dedupe` |

---

## Goal

`/track.js` (Change 14) sends a beacon on every `pushState`/`replaceState`, including calls that
leave the URL unchanged. Wiring the snippet into infraege.ru (a TanStack Router site, infraegev2
Change 133) showed two `POST /api/collect` per page load: the router calls `replaceState` with the
same URL during hydration. Every load was counted twice. This change makes the snippet send only
when `pathname + search` differs from the last URL it sent (SPEC.md §4i). The change is shipped
before infraegev2 Change 133.

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
- [x] `B1` Move the snippet out of the Go string into an embedded
      `internal/analytics/interfaces/http/track.js` (`go:embed`) and make it remember the last sent
      `pathname + search`. `send()` goes out only when the current URL differs, for the initial load,
      `pushState`, `replaceState` and `popstate` alike. A hash-only change is not a pageview. Wire
      format, headers and `GET /track.js` behavior are unchanged. — _Depends on:_ —
- [x] `B2` Behavior test `track_test.go`: run the served snippet under `node` with a minimal
      browser shim (`document.currentScript`, `location`, `history`, `navigator.sendBeacon`,
      `window` events) and count beacons. Cases: initial load → 1; same-URL `replaceState`
      (hydration) → no beacon; `pushState` to a new path → beacon; `replaceState` that changes the
      search → beacon; `popstate` back to an earlier URL → beacon; hash-only `pushState` → no
      beacon; beacon body carries the new URL. Skip with an explicit message only when `node` is
      absent (CI installs it before `go test`). — _Depends on:_ B1

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: client routers call `replaceState` with the same URL, so a
      history call is not a pageview. SPEC.md §4i was updated by `/plan`. — _Depends on:_ B1

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/analytics/interfaces/http/track.go
internal/analytics/interfaces/http/track.js        (new, embedded)
internal/analytics/interfaces/http/track_test.go   (new)
docs/KNOWN_GOTCHAS.md
docs/SPEC.md                                        (§4i, done by /plan)
~~~

### Do NOT touch
- `POST /api/collect` handler, storage, stats, dashboard UI — the fix is entirely client-side.
- The infraegev2 repository (its Change 133 ships separately, after this one).

---

## Contracts

See `docs/SPEC.md` §4i and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../STACK.md) — this section only records
> change-specific overrides.

```bash
# Live check after deploy (served snippet contains the URL guard):
curl -s https://sre.infraege.ru/track.js | grep -c 'last'
# Real-browser check against infraegev2 dev server (Change 133 branch): exactly one
# POST /api/collect per page load; one more per in-app navigation to a new URL.
```

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

None

---

## Commit Message

```
fix(change-16): count a pageview per URL change, not per history call
```
