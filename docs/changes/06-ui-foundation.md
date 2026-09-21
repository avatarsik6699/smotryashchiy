# CHANGE 06 — UI Foundation

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `06` |
| Slug | `ui-foundation` |
| Title | UI Foundation |
| Status | `active` |
| Branch | `feature/06-ui-foundation` |

---

## Goal

Stage 3, part 1: everything the single-page dashboard (Change 07) stands on. The Go binary gets the
missing UI API (`GET/POST /api/hosts`, `step` downsampling), serves an embedded SPA with security
headers and a corrected auth gating rule, and a `web/` project (Vite, React, TypeScript strict, Base UI,
uPlot, design tokens) ships a working login screen and empty shell, with frontend gates in CI and the
Full Gate. See `docs/SPEC.md` §4d and §5, and the approved plan
`/home/niquetamerewsl/.claude/plans/ui-swift-octopus.md`. No dashboard content, charts, stream handling
or add-host dialog (Change 07).

---

## Design References

`docs/reference/DESIGN.md` (tokens and rules), `docs/reference/PRODUCT.md`,
`docs/reference/ui-references/` (predecessor screens, frozen donor; this change builds only the login
screen and the empty command bar).

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Backend
- [x] `B1` `GET /api/hosts`: store list (`hosts` ordered by name, `last_seen_at` null until first batch), application service, handler, JSON per SPEC §4d — _Depends on:_ —
- [x] `B2` `POST /api/hosts` (201 `{host, server_url, secret, expires_at}`; 409 duplicate, 400 invalid name) reusing `EnrollmentService.CreateHost`; config `SMOTRYASHCHIY_PUBLIC_URL` (validated http(s) URL, required in production) with request-derived fallback — _Depends on:_ B1
- [x] `B3` `step=N` on `GET /api/metrics` (10–3600 s, raw only, rejected with `latest`/`resolution=hour`): newest sample per N-second bucket per series, ordering and limit semantics preserved — _Depends on:_ —
- [x] `B4` Static delivery: `web/embed.go` (`go:embed all:dist`, committed stub `dist/index.html`), handler with SPA fallback (never for `/api/*`), `no-cache` index, immutable hashed assets, CSP/nosniff/referrer headers; mounted in `server.go` — _Depends on:_ —
- [x] `B5` Refactor auth gating (`internal/auth/interfaces/http/middleware.go`): paths outside `/api/` and the health probes are public, everything under `/api/` needs a session except `login` and `enroll`; unauthenticated unknown `/api/*` stays `401` — _Depends on:_ B4
- [x] `B6` Contract tests: hosts list/create semantics (secret shown once, only its hash stored, ordering, unknown stays null), `step` bucketing and validation, static headers/fallback/cache, gating on both sides (static without session OK, `/api/*` without session 401, `/api/nope` with session 404 JSON not HTML), real-binary smoke that the page loads — _Depends on:_ B1, B2, B3, B5

- [x] `B7` `GET /api/auth/session` (public, always `200 {"authenticated":bool}`): lets the SPA learn whether a session exists without a red 401 in the browser console on every logged-out load (found in the real-browser check) — _Depends on:_ B5

### Frontend
- [x] `F1` Scaffold `web/`: Vite + React + TypeScript strict + `@base-ui/react` + `uplot`, CSS Modules, `tokens.css` from DESIGN.md (dark default, `prefers-color-scheme` light), portal root isolation per Base UI quick start, npm lockfile, dev proxy to the Go server (incl. WebSocket), Vitest + Testing Library, `.gitignore` keeps the stub `dist/index.html` — _Depends on:_ B4
- [x] `F2` API client: typed fetch wrapper, uniform error type, `401` → session-expired signal, `Retry-After` handling for `429`; session state hook — _Depends on:_ F1
- [x] `F3` Login screen (Base UI Form/Field/Input/Button) shown in place on `401`; wrong password, rate-limited and network-error states are distinct text; empty command bar with `logout` after login — _Depends on:_ F2
- [x] `F4` Component tests (login states, API client 401/429) with `vitest-axe` accessibility checks — _Depends on:_ F3

### Infra
- [x] `I1` Gates and CI: STACK.md Fast/Full Gate rows (typecheck, `npm ci`, tests, build before `go build`), `scripts/bundle-budget.sh` (JS ≤ 200 KB gzip), `actions/setup-node` and web steps in `.github/workflows/ci.yml`; `go build ./...` must still pass with only the stub `dist/` — _Depends on:_ F1

### Other
- [x] `T1` Docs: STACK.md frontend conventions (structure, commands, env var `SMOTRYASHCHIY_PUBLIC_URL`), KNOWN_GOTCHAS; real-browser verification with Playwriter (login, wrong password, `401` view, console clean, no CSP violations, `HttpOnly` cookie, screenshot at 360/1280 px) recorded in Implementation Notes — _Depends on:_ B6, F4, I1

---

## Files

### Create / modify
~~~
internal/telemetry/(application|infrastructure|interfaces/http)/   (hosts list/create, step)
internal/platform/config/                                          (PUBLIC_URL)
internal/auth/interfaces/http/middleware.go                        (gating refactor)
internal/platform/httpserver/ or web/embed.go                      (static handler)
cmd/smotryashchiy/server.go                                        (mount hosts + static)
web/                                                               [new: Vite app]
scripts/bundle-budget.sh                                           [new]
.github/workflows/ci.yml
docs/STACK.md, docs/KNOWN_GOTCHAS.md
~~~

### Do NOT touch
- Dashboard sections, charts usage, WebSocket client, add-host dialog (Change 07)
- Agent, transport, ingest, enrollment endpoints, rollups/retention
- `docs/reference/` (frozen donor)

---

## Contracts

See `docs/SPEC.md` §4.3, §4d, §5 and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: build the frontend and the Go binary, start the server, open
`/` in the real browser via Playwriter, log in with the development password, and confirm the shell
renders, the console has no errors or CSP violations, a wrong password shows its error, and
`GET /api/hosts` answers `200` with a session and `401` without.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Stub layout differs from the plan:** `web/dist` tracks only `.gitkeep`; `web/placeholder.html` is embedded
  separately and served when `dist/index.html` is absent. A Vite plugin (`keep-dist-placeholder`) restores
  `.gitkeep` after each build because Vite empties `outDir`.
- **B7 added mid-session** (own finding from the real-browser check): `GET /api/auth/session`, public and
  always 200, replaces probing `/api/hosts` so a logged-out load leaves no red 401 in the console.
- **Gating rule changed** (B5): everything outside `/api/` is public. Unauthenticated `/api/anything`
  is still 401; with a session, unclaimed `/api/*` paths answer JSON 404 from the static handler.
- **Real-browser verification (T1).** Server = real binary serving the built UI.
  Playwriter (user's Chrome, dark and light via `emulateMedia`, 360 and 1280 px): login and shell render,
  no horizontal scroll in any combination, screenshots reviewed. The user's Chrome injects foreign
  extension styles that trip our strict CSP, so console cleanliness was judged in a clean profile
  (Playwright MCP, `--disable-extensions`; used as an allowed fallback because Playwriter's headless mode
  would need a browser download): 0 console errors on load, exactly one error after a wrong password (the
  genuine 401 from `POST /api/auth/login`), no CSP violations, session cookie not readable from JS
  (`document.cookie` empty while `/api/auth/session` says authenticated), wrong password shows
  "Wrong password." and can be retried, `GET /api/hosts` 200 with a session and 401 without.
- Real-browser testing found three defects the unit tests could not: the `invalid`-prop resubmit
  deadlock, the console 401 on load (-> B7) and the missing favicon (404).
- Node/npm toolchain versions resolved by npm at scaffold time: Vite 8.3, React 19.3, TypeScript 7.0,
  Vitest 5, `@base-ui/react` 1.8, uplot 1.6. TS 7 works with the config as-is.
- Bundle: 247 KB raw, 78 KB gzip JS (budget 200 KB). `uplot` is installed but unused until Change 07.

---

## Commit Message

```
feat(change-06): UI API, embedded SPA, web scaffold and login
```
