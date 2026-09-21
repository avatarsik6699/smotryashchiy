# Stack Guide

> **Source of truth for concrete technologies, commands and gates.** `docs/playbooks/work.md` reads
> the Fast Gate and Required Tooling tables; `docs/playbooks/ship.md` reads the Full and Release
> Gate tables. Keep them accurate. Rows for areas that do not exist yet are `n/a` and get filled by
> the change that introduces the area.
>
> **Stack status:** MINIMAL (Go backend, agent, and the web UI foundation; dashboard and deploy pending their changes)

## Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26.6 minimum (`go.mod`; earlier patches carry fixed stdlib CVEs), single binary with `server` / `agent` / `admin` modes |
| Database | SQLite via `modernc.org/sqlite` (pure Go, WAL, single writer), embedded forward-only migrations |
| Realtime | `github.com/coder/websocket` (server-to-client stream at `/api/stream`) |
| Transport | `golang.zx2c4.com/wireguard` (userspace WireGuard + gVisor netstack; pure Go, no root, no kernel module) |
| Frontend | Embedded static SPA in `web/`: Vite 8, React 19, TypeScript 7 (strict), `@base-ui/react` (Form/Field/Button, later Accordion/Dialog/Tooltip), `uplot` (charts), CSS Modules, npm; tests: Vitest + Testing Library + vitest-axe |
| Transport | Userspace WireGuard inside the binary — pending Stage-2 spike |
| CI | GitHub Actions (`.github/workflows/ci.yml`) on pull requests and pushes to `main` |

## Setup

```bash
go version                 # >= 1.26.6 (go.mod triggers an automatic toolchain download if older)
cp .env.example .env       # optional; every variable has a development default
go run ./cmd/smotryashchiy admin host create --name vps-1   # prints the one-time agent enroll command
go run ./cmd/smotryashchiy agent run --config agent.json --interval 10s   # after `agent enroll`; Linux only
go run ./cmd/smotryashchiy server   # prints a one-time development admin password
printf '%s\n' "$PASSWORD" | go run ./cmd/smotryashchiy admin set-password   # >= 24 bytes, stdin only
```

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `SMOTRYASHCHIY_RAW_RETENTION_DAYS` | `30` | TTL of raw metrics, checks, events and ingestion batches |
| `SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS` | `396` | TTL of hourly metric rollups (13 months) |
| `SMOTRYASHCHIY_WG_PORT` | `51820` | UDP port of the in-process WireGuard endpoint |
| `SMOTRYASHCHIY_TUNNEL_CIDR` | `10.99.0.0/16` | Tunnel subnet (IPv4, /8../24); the server takes its first host address |
| `SMOTRYASHCHIY_PUBLIC_ENDPOINT` | dev: `127.0.0.1:<udp port>` | Agent-facing `host:port` of the WireGuard UDP port; **required in production** |
| `SMOTRYASHCHIY_PUBLIC_URL` | derived from the request | `http(s)` base URL agents use to enroll (shown in the UI's enrollment command); **required in production** |
| `SMOTRYASHCHIY_DEV_BACKEND` | `http://127.0.0.1:8080` | (`web/`, dev only) Go server the Vite dev proxy forwards `/api` and WebSocket to |

Other variables (`SMOTRYASHCHIY_ADDR`, `_DB_PATH`, `_PRODUCTION`, `_RELEASE`, `_SECURE_COOKIES`,
`_TRUSTED_PROXY_CIDRS`) are documented in `internal/platform/config`.

## Fast Gate

| Check | Command | Notes |
|-------|---------|-------|
| Lint | `test -z "$(gofmt -l .)" && go vet ./...` | |
| Unit tests | `go test ./... -short` | |
| Frontend typecheck | `npm --prefix web run typecheck` | only when `web/` changed |
| Frontend tests | `npm --prefix web test` | only when `web/` changed; includes axe accessibility checks |
| LSP diagnostics | `no — gopls not configured` | informational |

## Full Gate

| Check | Command | Notes |
|-------|---------|-------|
| Formatting / static analysis | `test -z "$(gofmt -l .)" && go vet ./...` | |
| Module integrity | `go mod verify` | |
| Frontend install | `npm --prefix web ci` | lockfile install |
| Frontend typecheck | `npm --prefix web run typecheck` | `tsc --noEmit`, strict |
| Frontend tests / a11y | `npm --prefix web test` | Vitest; `vitest-axe` checks components |
| Frontend build | `npm --prefix web run build` | writes `web/dist`, which `go build` embeds |
| Bundle budget | `bash scripts/bundle-budget.sh` | gzip JS <= 200 KB (`BUNDLE_BUDGET_KB` overrides) |
| Backend tests | `go test ./...` | |
| Smoke | `go build ./...` | builds with the freshly built UI embedded |
| Secrets scan (Gitleaks) | `bash scripts/secrets-gate.sh` | Pins Gitleaks v8.30.1; scans full git history and non-ignored working files |
| Dependency audit | `bash scripts/vuln-gate.sh` | Pins govulncheck v1.8.0; fails on reachable vulnerabilities |
| E2E / browser | `n/a` | real-browser checks are run with Playwriter per change (see the change's Gate Checks); a scripted suite is not added yet |

## Release Gate

| Check | Command | Notes |
|-------|---------|-------|
| `gh` authenticated | `gh auth status` | required before pushing |
| Image scan / deploy verification | `n/a` | added in Stage 6 |

## Required Tooling

| Domain | Required tool/skill | When | Available |
|--------|---------------------|------|-----------|
| Frontend UI change | Playwriter (Playwright MCP fallback): screenshot + console check | before checking off | `yes` |
| Go change | LSP diagnostics | before checking off | `no` — report as skipped |
| Frontend UI/design decision | `impeccable` skill | `/plan` and design items | `yes` |
| Library API use | Context7 | before writing library code | `yes` |

## Project structure

```
cmd/smotryashchiy/   # single entrypoint, mode subcommands
internal/platform/   # config, db (migrations), httpserver, apierror
internal/auth/       # admin password, sessions (domain/application/infrastructure/interfaces)
internal/transport/  # userspace WireGuard server/client wrappers (keys, peers, tunnel listener)
internal/agent/      # agent: enroll, persistent tunnel session, sender (retry/backoff), run loop, batch builder
internal/agent/collect/  # /proc + statfs collectors (cpu, memory/swap, disk, network, load, uptime)
internal/agent/spool/    # durable bounded FIFO of unsent batches (offline buffer)
web/                 # single-page UI (Vite app) + embed.go (go:embed all:dist, static handler, CSP)
internal/telemetry/  # Metric/Check/Event contract, ingest service, SQLite store, read API,
                     # hourly rollups + retention jobs (Maintenance), live stream hub + WebSocket
                     # bounded contexts talk through ports (application interfaces), not internals
docs/                # SPEC, STACK, playbooks, changes/, reference/ (predecessor design donors)
```

## Frontend conventions

- One page, no router (SPEC §5). Design tokens live in `web/src/styles/tokens.css` (from
  `docs/reference/DESIGN.md`); components use CSS Modules and token variables only, no hard-coded colors.
- Use Base UI (`@base-ui/react/<component>`) for every control it offers; wrap only for styling.
  Do not pass `invalid` to `Field.Root` for server-side errors (it blocks resubmission); use `aria-invalid` + text.
- All network access goes through `src/api/client.ts` (`ApiError` kinds, `401` signal, `Retry-After`).
- Dev: `npm --prefix web run dev` (Vite proxies `/api` incl. WebSocket to `SMOTRYASHCHIY_DEV_BACKEND`;
  the proxy keeps the Host header so the WebSocket same-origin check passes). Production: `npm run build`
  then `go build`; `web/dist/.gitkeep` is the only tracked file there, and an unbuilt binary serves a
  placeholder page.
- Charts: only through `components/Chart/Chart.tsx` (uPlot). New chart types must keep the layout contract of
  SPEC §5 and are checked in a real browser at 360/768/1280/1920 px (structure: every descendant inside the
  chart box, no horizontal page scroll; visual: axis labels and tooltip not clipped).
- Layout: grids that contain charts use `minmax(0, 1fr)` tracks; rows stack at 900 px, events at 720 px.
- Tests: Vitest with jsdom (no canvas: uPlot is mocked in unit tests; real chart behavior is checked in the
  browser with Playwriter). Every screen gets an axe check.
