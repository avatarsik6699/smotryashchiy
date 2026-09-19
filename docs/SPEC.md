# TECHNICAL SPECIFICATION (SPEC.md): `smotryashchiy`

> **For AI agent**: Read this file in full before any change. Approved by the architect on
> 2026-09-19.

## Metadata

| Field | Value |
|-------|-------|
| Document Version | `v1.0` |
| Date | `2026-09-19` |
| Architect / Owner | `avatarsik666@gmail.com` |
| Stack | See [docs/STACK.md](./STACK.md) |
| Domain | Self-contained self-hosted monitoring for solo developers and small teams |
| Predecessor | `sre-kit` (frozen donor repo: contract, ingest, alerting, auth, UI language) |

---

## 1. Overview and Goals

### 1.1 Problem

The predecessor aggregated independent open-source tools (Beszel, Umami, journal-gatewayd,
fail2ban, SSH) through adapters. Each integration required accounts, network access, provisioning
and upkeep; a large share of changes were hotfixes at adapter seams. Setup was too heavy to be
worth it.

### 1.2 Goal

One product that ships every needed capability itself. **Success metric:** on a clean VPS an
operator gets a working server and the first monitored host in under 5 minutes, with two commands,
no third-party accounts, no external database, and no SSH credentials stored in the product.

Principles: simplicity of install and upkeep over breadth; built-in over integrated; unknown never
looks healthy; observe only.

### 1.3 Boundaries

| Included (MVP) | Excluded |
|----------|----------|
| Single Go binary: `server`, `agent`, `admin` modes | Remote actions, deployment, config mutation of targets |
| Host metrics via own agent (CPU, RAM, swap, disk, network, load, uptime) | Adapters/plugins for third-party tools |
| Docker container metrics via the local socket | Web analytics (deferred to a later change) |
| Built-in uptime checks: HTTP, TCP, TLS expiry | Multi-user / RBAC / audit log |
| Log/event collection: journald, Docker logs | Second notification channel |
| Security signals: fail2ban bans and jail status | Distributed / high-cardinality storage |
| Alert rules, firing → resolved lifecycle, Telegram | Off-host backup (later) |
| Embedded static web UI, single admin password | Escalation / repeat notifications |

---

## 2. Domain

Entities (carried over from the predecessor's proven contract):
`Project → Host | Check target → (Metric | Check | Event) → Alert`

- **Host** — a machine running an agent (identity, enrollment, last-seen).
- **Uptime target** — a URL/endpoint probed by the server itself.
- **Metric** — `{name, ts, value, labels}` time series. **Check** — `{name, ts, status: ok|warn|critical, meta}`.
  **Event** — `{ts, level, message, labels}`. **Alert** — derived by the router from rules.

Contract rules (from predecessor lessons, see `docs/KNOWN_GOTCHAS.md`): zero is a valid value;
producer timestamps are validated (≤5 min in the future); idempotent batches; replays are stored
once and never re-alerted. Additive-only versioning after v1.

## 3. Architecture

```text
 host A                          server (one binary/container)              operator
 ┌──────────┐  WireGuard tunnel  ┌───────────────────────────────┐  HTTPS   ┌─────────┐
 │  agent   │ ─────────────────▶ │ ingest → SQLite → alerts → WS │ ◀──────▶ │ browser │
 └──────────┘  (userspace, in    │ uptime prober · embedded SPA  │  (ACME)  └─────────┘
               the binary)       └───────────────────────────────┘
```

- **One binary, three modes.** `server`, `agent`, `admin` (stdin-only password set, backup,
  telemetry reset). Collectors are ordinary internal Go packages; there is no adapter subprocess
  protocol, manifest layer or third-party credential store.
- **Agent transport: WireGuard, implemented in userspace inside the binary**
  (`wireguard-go` + netstack). It needs no kernel module, no `wireguard-tools`, no root for the
  tunnel and no firewall changes beyond one UDP port on the server. The agent is pinned to the
  server's key, so identity and encryption come from the tunnel; ingest is reachable only inside
  it. *Fallback if the Stage-2 spike disproves this:* HTTPS push with a per-host bearer token.
- **Enrollment.** The UI creates a host and shows one command containing the server endpoint and a
  one-time enrollment secret; the agent generates its own keypair, registers its public key, and
  receives a tunnel address. No SSH, no passwords.
- **Storage.** SQLite (WAL): raw records with a 30-day TTL, hourly rollups with a 13-month TTL,
  idempotent ingestion batches; reuse the predecessor's proven semantics.
- **UI.** Static SPA embedded with `go:embed`, served by the same process; live updates over
  WebSocket. No separate web server or SSR container.
- **TLS.** Built-in ACME (certmagic) for the UI origin; no separate proxy container.
- **Auth.** Single admin password (bcrypt), HttpOnly/SameSite=Lax session cookie, failed-login
  rate limit, hash initialized only via the stdin-only `admin` command.

## 4. Interfaces (to be detailed per change)

REST + WS for the UI, ingest over the tunnel, `/healthz` and `/health/ready` (exact release SHA).
The API surface is specified in the change that introduces it; the codebase is then the source of
truth.

## 5. UI

Reuses the predecessor's visual authority: `docs/reference/PRODUCT.md`, `docs/reference/DESIGN.md`
and `docs/reference/ui-references/` (dark, compact, mono-first terminal vocabulary; status never
color-only; WCAG 2.2 AA). Pages: Dashboard, Hosts, Host detail, Uptime, Alerts/Notifications, Login.

## 6. Non-Functional

Security: no hardcoded secrets, secrets scan and dependency audit in the Full Gate, non-root
images. Backup: `admin` command produces a consistent SQLite snapshot bundle. Live-update latency
within a few seconds. Agent footprint: small enough to run on the smallest VPS.

## 7. Roadmap

| Stage | Change scope |
|-------|--------------|
| 0 | Repo, SPEC/STACK, first change: skeleton (binary, SQLite, migrations, auth, CI) |
| 1 | Data core: contract, ingest, storage, retention/rollup, WS |
| 2 | Agent v1: host metrics, WireGuard transport (spike first), enrollment |
| 3 | UI v1: Dashboard, Hosts, Host detail (embedded SPA) |
| 4 | Uptime prober, alert rules, Telegram |
| 5 | Docker, logs, fail2ban in the agent |
| 6 | Distribution: image/binary, ACME, backup, deploy workflow, runbook |
| 7 | Dogfood on a fresh VPS for infraegev2; derive v2 backlog (incl. analytics) |

## 8. Open Questions

- Stage-2 spike: confirm userspace WireGuard (netstack) is reliable across common VPS/NAT setups;
  otherwise fall back to HTTPS push.
- Frontend stack for the embedded SPA (predecessor used React + Base UI + uPlot + Vite): confirm in
  the UI change.
- Supported agent platforms: Linux amd64/arm64 for MVP (assumption).
