# Stack Guide

> **Source of truth for concrete technologies, commands and gates.** `docs/playbooks/work.md` reads
> the Fast Gate and Required Tooling tables; `docs/playbooks/ship.md` reads the Full and Release
> Gate tables. Keep them accurate. Rows for areas that do not exist yet are `n/a` and get filled by
> the change that introduces the area.
>
> **Stack status:** MINIMAL (Go skeleton with auth and CI; frontend and deploy pending their changes)

## Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26.6 minimum (`go.mod`; earlier patches carry fixed stdlib CVEs), single binary with `server` / `agent` / `admin` modes |
| Database | SQLite via `modernc.org/sqlite` (pure Go, WAL, single writer), embedded forward-only migrations |
| Realtime | `github.com/coder/websocket` (server-to-client stream at `/api/stream`) |
| Transport | `golang.zx2c4.com/wireguard` (userspace WireGuard + gVisor netstack; pure Go, no root, no kernel module) |
| Frontend | Embedded static SPA — pending (see SPEC §8) |
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

Other variables (`SMOTRYASHCHIY_ADDR`, `_DB_PATH`, `_PRODUCTION`, `_RELEASE`, `_SECURE_COOKIES`,
`_TRUSTED_PROXY_CIDRS`) are documented in `internal/platform/config`.

## Fast Gate

| Check | Command | Notes |
|-------|---------|-------|
| Lint | `test -z "$(gofmt -l .)" && go vet ./...` | |
| Unit tests | `go test ./... -short` | |
| LSP diagnostics | `no — gopls not configured` | informational |

## Full Gate

| Check | Command | Notes |
|-------|---------|-------|
| Formatting / static analysis | `test -z "$(gofmt -l .)" && go vet ./...` | |
| Module integrity | `go mod verify` | |
| Backend tests | `go test ./...` | |
| Smoke | `go build ./...` | |
| Secrets scan (Gitleaks) | `bash scripts/secrets-gate.sh` | Pins Gitleaks v8.30.1; scans full git history and non-ignored working files |
| Dependency audit | `bash scripts/vuln-gate.sh` | Pins govulncheck v1.8.0; fails on reachable vulnerabilities |
| Frontend / E2E / a11y | `n/a` | added with the UI change |

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
internal/telemetry/  # Metric/Check/Event contract, ingest service, SQLite store, read API,
                     # hourly rollups + retention jobs (Maintenance), live stream hub + WebSocket
                     # bounded contexts talk through ports (application interfaces), not internals
docs/                # SPEC, STACK, playbooks, changes/, reference/ (predecessor design donors)
```
