# Stack Guide

> **Source of truth for concrete technologies, commands and gates.** `docs/playbooks/work.md` reads
> the Fast Gate and Required Tooling tables; `docs/playbooks/ship.md` reads the Full and Release
> Gate tables. Keep them accurate. Rows for areas that do not exist yet are `n/a` and get filled by
> the change that introduces the area.
>
> **Stack status:** MINIMAL (Go skeleton only; frontend and infra pending their changes)

## Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26 (`go.mod`), single binary with `server` / `agent` / `admin` modes |
| Database | SQLite (pure-Go driver, decided in Change 01) |
| Frontend | Embedded static SPA — pending (see SPEC §8) |
| Transport | Userspace WireGuard inside the binary — pending Stage-2 spike |
| CI | GitHub Actions (added in Change 01) |

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
| Secrets scan | `n/a` | add in Change 01 (config in `.gitleaks.toml`) |
| Dependency audit | `n/a` | add in Change 01 (`govulncheck`) |
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
internal/            # bounded contexts, ports between them (no cross-imports of internals)
docs/                # SPEC, STACK, playbooks, changes/, reference/ (predecessor design donors)
```
