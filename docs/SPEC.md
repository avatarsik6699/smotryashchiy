# TECHNICAL SPECIFICATION (SPEC.md): `smotryashchiy`

> **For AI agent**: Read this file in full before any change. Approved by the architect on
> 2026-09-19.

## Metadata

| Field | Value |
|-------|-------|
| Document Version | `v1.9` |
| Date | `2026-09-21` |
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
| ~~Alert rules, firing → resolved lifecycle, Telegram~~ *(deferred, see §7)* | Off-host backup (later; local snapshot bundle in §4f) |
| Embedded static web UI, single admin password | Escalation / repeat notifications |

---

## 2. Domain

Entities (carried over from the predecessor's proven contract):
`Project → Host | Check target → (Metric | Check | Event) → Alert`

- **Host** — a machine running an agent (identity, enrollment, last-seen).
- **Uptime target** — a URL/endpoint probed by the server itself (§4e); its results are their own
  records, not host telemetry.
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

## 4. Telemetry contract and storage (Changes 02–03)

### 4.1 Wire batch (schema_version `1.0`, additive-only afterwards)

```json
{"schema_version":"1.0",
 "metrics":[{"name":"cpu.usage_percent","ts":"2026-09-19T12:00:00Z","value":0,"labels":{"core":"0"}}],
 "checks":[{"name":"disk.root","ts":"2026-09-19T12:00:00Z","status":"ok","meta":{}}],
 "events":[{"ts":"2026-09-19T12:00:00Z","level":"warn","message":"ban 203.0.113.7","labels":{"jail":"sshd"}}]}
```

Validation (rejects the whole batch with a field-addressed error; nothing is partially stored):
- `name`: `^[a-z][a-z0-9_.]{0,127}$`. `value`: finite number; **zero is valid and must be stored**.
- `status`: `ok|warn|critical`. `level`: `info|warn|error|critical`. `message` ≤ 2048 bytes.
- `labels`: ≤ 16 keys, key `^[a-z][a-z0-9_]{0,63}$`, value ≤ 128 bytes. `meta`: JSON object ≤ 4 KiB.
- `ts`: RFC 3339, normalized to UTC; producer time more than 5 minutes in the future is rejected.
- ≤ 1000 records per batch; `Idempotency-Key` ≤ 128 bytes.

### 4.2 Storage semantics

Tables `hosts` (id, name, created_at, last_seen_at), `metrics`, `checks`, `events`,
`ingestion_batches (host_id, idempotency_key, received_at, record_count)`; every telemetry row is
attributed to a host and carries `schema_version`. Record identity, enforced by unique indexes:
metric = (host, name, canonical labels, ts); check = (host, name, ts); event = (host, ts, level,
message, canonical labels). A batch is applied in one transaction. Re-sending an
`Idempotency-Key` is a no-op reported as `replayed`. Overlapping records from a *different* batch
are stored once and reported as `duplicates`, so later alert evaluation can skip them. A stored
batch advances `hosts.last_seen_at` to the receipt time; producer `ts` never does.

### 4.3 Read API (session required, read-only)

| Verb | Path | Behavior |
|------|------|----------|
| GET | `/api/metrics?host=&name=&from=&to=&limit=&latest=&step=` | time series ordered by `ts` ascending, `limit` default 1000, max 5000; `latest=true` returns the newest point per (name, labels) and is incompatible with `from`/`to`; `step=N` (10–3600 s, raw resolution only, incompatible with `latest`) keeps only the newest sample of every N-second bucket per series (a sampled series, valid for gauges and counters alike) |
| GET | `/api/checks?host=&name=` | newest Check per (host, name) |
| GET | `/api/events?host=&level=&limit=` | newest first, `limit` default 100, max 500 |

`GET /api/metrics` gains `resolution=raw|hour` (default `raw`). `hour` reads rollups (§4.4), returns
`{"rollups":[{host, name, labels, ts, count, min, max, avg}]}`, one per point (`ts` = hour start, UTC), and is the only resolution that
reaches past the raw TTL; `latest=true` is raw-only.

### 4.4 Rollups (Change 03)

Table `metric_rollups_hourly (host_id, name, labels_json, hour_ts, count, min, max, sum)`, primary
key = (host, name, canonical labels, hour_ts). Only metrics are rolled up; checks and events are not.
A rollup job aggregates every *closed* hour from raw rows, idempotently (recomputing an hour
overwrites it), on start-up (catching up from the last rolled-up hour) and then hourly. Late data for
an already-rolled hour is picked up by re-aggregating the hours touched in the last 24 h on each run.
Zero is a value: a stored `0` contributes to `count`, `min`, `max` and `sum`.

### 4.5 Retention (Change 03)

Raw metrics, checks and events: 30 days. Hourly rollups: 13 months. Enforced by a purge job run at
start-up and daily, deleting in bounded batches so the single SQLite writer is never blocked for long.
A raw row is deleted only after its hour has been rolled up. `ingestion_batches` are purged with the
raw TTL. TTLs are configurable (`SMOTRYASHCHIY_RAW_RETENTION_DAYS`,
`SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS`); non-positive or unparsable values fail start-up.

### 4.6 Live stream (Change 03)

`GET /api/stream` upgrades to WebSocket; requires the session cookie and a same-origin `Origin`
header (otherwise `401`/`403`, no upgrade). Server-to-client only, JSON text frames:
`{"type":"metric|check|event","host_id":"…","record":{…}}` carrying only records **newly accepted**
by ingest (duplicates and replays are never published). Uptime results (§4e) travel as
`{"type":"uptime","target_id":"…","record":{…}}` (additive; a subscription filtered by `host` does not receive them). Optional query filters `host` and `type`.
Delivery is best-effort and non-blocking: a slow client with a full buffer (64 messages) is
disconnected, never allowed to stall ingest; clients recover state via the read API on reconnect.
The server sends ping every 30 s and drops unresponsive connections. Ingest stays HTTP-only and lands
in Stage 2; Change 03 publishes from the ingest service so that stage needs no stream work.

## 4b. Transport, enrollment and ingest (Change 04)

**Transport spike (passed 2026-09-21).** Change 04 opened with a spike proving userspace WireGuard
(`wireguard-go` + gVisor netstack, no kernel module, no root) carries HTTP between a server and an
agent in one Go test. Verdict recorded in the change file. If the spike fails, work stops for an
architect decision on the §3 fallback (HTTPS push with a per-host bearer token); the contracts
below (enroll → host identity → `POST /api/ingest`) stay the same, only the identity mechanism
changes. Real-NAT reliability cannot be proven locally and is verified on a VPS (Stage 7).

**Tunnel.** The server owns a WireGuard device (UDP `SMOTRYASHCHIY_WG_PORT`, default `51820`)
inside the process, address `.1` of `SMOTRYASHCHIY_TUNNEL_CIDR` (default `10.99.0.0/16`). Its private
key is generated once and stored in the `settings` table; the public key is shown by enrollment. Peers
are added at runtime on enrollment and re-added from storage at start-up. Each peer's `AllowedIPs`
is exactly its own `/32`.

**Enrollment.**
- `admin host create --name N` (stdin-free, prints once): creates the host and a one-time
  enrollment secret (32 random bytes, base64url; only its SHA-256 is stored; valid 1 h, single
  use) and prints the agent command `smotryashchiy agent enroll --server URL --secret S`.
- `POST /api/enroll` (public, per-IP rate limited; body `{secret, public_key}`) atomically consumes
  the secret, assigns the next free tunnel address, adds the peer and returns
  `{host_id, tunnel_ip, server_public_key, server_endpoint, server_tunnel_ip}`. Wrong, expired or
  used secrets all answer the same `401`. A public key already enrolled answers `409`.
- The agent generates its own keypair (private key never leaves the host, stored `0600`).
- Tables: `host_enrollments (host_id, secret_hash, expires_at, used_at)`,
  `host_peers (host_id PK, public_key UNIQUE, tunnel_ip UNIQUE, enrolled_at)`.

**Ingest.** `POST /api/ingest` is served **only on the tunnel listener** (`server_tunnel_ip:8443`,
plain HTTP; encryption and peer identity come from WireGuard), never on the public address. Headers:
`Idempotency-Key`; body is the §4.1 batch. The host is derived from the connection's source tunnel
address via `host_peers`, never from a client-supplied ID. Responses: `200`
`{accepted:{metrics,checks,events}, duplicates:{…}, replayed}`, `400` field-addressed validation
error, `413` body over 1 MiB, `429` when a host exceeds 10 batches/s. It calls the same
`Service.Ingest` as tests and publishes accepted records to the live stream (§4.6).

**Agent (Change 04).** Only `agent enroll` (keypair, enrollment call, writes
`agent.json` config `0600`) and a `agent push-file FILE` helper that sends a §4.1 batch through the
tunnel; collectors and the run loop are Change 05.

## 4c. Agent runtime and metric catalog (Change 05)

`smotryashchiy agent run [--config PATH] [--interval 10s] [--spool-dir DIR]` is the long-running
agent (Linux amd64/arm64; other platforms exit with a clear error). It keeps **one persistent
tunnel**, and every `interval` (default 10 s, allowed 5 s–5 min) collects one batch (§4.1) and sends
it to `POST /api/ingest`. Stopping on SIGINT/SIGTERM is graceful: the batch in flight is spooled, not
lost.

**Collectors** read `/proc` and `statfs` directly (no third-party agent library, small footprint).
A collector that cannot read its source omits its metrics for that tick and logs once per failure
streak; it never emits a fabricated `0` (unknown stays unknown; a *measured* zero is emitted).

| Metric | Labels | Meaning |
|--------|--------|---------|
| `cpu.usage_percent` | — | 0–100, non-idle share of all CPUs since the previous tick (iowait counts as idle); the first tick emits none |
| `memory.total_bytes`, `memory.used_bytes`, `memory.used_percent` | — | used = total − `MemAvailable` |
| `swap.total_bytes`, `swap.used_bytes`, `swap.used_percent` | — | a host without swap reports total 0 and used_percent 0 |
| `disk.total_bytes`, `disk.used_bytes`, `disk.used_percent` | `mount`, `device` | real filesystems only (ext2/3/4, xfs, btrfs, zfs, f2fs), one per device, ≤ 16 mounts; used_percent follows `df` (non-root-reserved blocks excluded) |
| `network.rx_bytes_total`, `network.tx_bytes_total` | `interface` | monotonically increasing counters excluding `lo`; consumers derive rates and handle counter resets |
| `load.avg_1m`, `load.avg_5m`, `load.avg_15m` | — | `/proc/loadavg` |
| `uptime.seconds` | — | `/proc/uptime` |

**Offline buffer.** Every batch is first written to a durable spool (directory, mode `0700`, files
`0600`, atomic write) together with its own `Idempotency-Key`, then sent oldest-first. A batch leaves
the spool only after a `200` (including `replayed`). Bounds: 5000 batches / 64 MiB, oldest dropped
first with a logged counter. Restart resumes from the spool.

**Retry policy.** Network errors, `429`, `5xx`, `401` → keep the batch, exponential backoff 1 s → 60 s
with jitter, rebuild the tunnel after 3 consecutive failures. `400`/`413` → the batch can never be
accepted: drop it, log the server's field-addressed error, continue. Producer time > 5 min ahead of
the server is a `400`; the agent logs a clock-skew hint.

## 4d. UI API and delivery (Change 06)

All routes below except static assets require the session cookie.

- `GET /api/auth/session` (public) → always `200 {"authenticated":true|false}`; it never answers `401`, so a
  logged-out load leaves no error in the browser console.
- `GET /api/hosts` → `{"hosts":[{"id","name","created_at","last_seen_at"}]}` ordered by name;
  `last_seen_at` is `null` until the first stored batch (unknown stays unknown).
- `POST /api/hosts` body `{"name"}` → `201 {"host":{id,name,created_at,last_seen_at},
  "server_url","secret","expires_at"}`. Same semantics as `admin host create` (secret shown once, valid
  1 h, `409` on a duplicate name, `400` on an invalid name). `server_url` is
  `SMOTRYASHCHIY_PUBLIC_URL` when set (validated `http(s)` URL, required in production), otherwise
  derived from the request (`https` when the request is TLS or cookies are `Secure`).
- `GET /api/metrics?step=N` as in §4.3.
- **Static delivery.** The SPA is built to `web/dist` and embedded (`go:embed`). A stub
  `dist/index.html` is committed so `go build` never needs node. Paths not under `/api/` and not a
  health probe are public static content (no secrets live in the bundle): `/` and unknown non-file paths
  return `index.html` (SPA fallback, `no-cache`), `/assets/*` are content-hashed (`immutable`, 1 year).
  Everything under `/api/` keeps §3's rules: session required except `POST /api/auth/login`,
  `GET /api/auth/session` and `POST /api/enroll`. Unknown `/api/*` paths answer `401`/`404` JSON, never `index.html`.
- **Security headers** on static responses: `Content-Security-Policy: default-src 'self';
  connect-src 'self'; img-src 'self' data:; style-src 'self'; frame-ancestors 'none'`,
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`.
- **Frontend gates.** Fast Gate adds `npm run typecheck`; Full Gate adds `npm ci`, typecheck, unit
  tests (with component-level axe checks) and the production build (before `go build`), plus a bundle
  budget of 200 KB gzip for JavaScript.

## 4e. Uptime prober (Change 08)

The server probes operator-defined **targets** itself; nothing runs on the monitored side.

- **Kinds.** `http` (absolute `http(s)` URL, no credentials, ≤ 2048 bytes): a `GET` following up to 5
  redirects, success = final status 200–399; the body is never read. `tcp` (`host:port`): the connection
  opens. `tls` (`host:port`): the TLS handshake completes with full certificate verification. `http`
  targets with an `https` URL and all `tls` targets also record the leaf certificate's `NotAfter`.
- **Definition.** `name` 1–80 bytes, unique; `interval_seconds` 30–3600 (default 60); per-check timeout
  10 s. At most 50 targets. Certificates are verified (self-signed or expired = a failed check with the
  verification error); probing internal addresses is allowed because only the authenticated operator defines
  targets.
- **Results** (`uptime_results`: target, `ts`, `ok`, `latency_ms`, `status_code`, `error`, `cert_expires_at`)
  are kept for the raw TTL (§4.5), purged independently of rollups. `latency_ms` is time to response headers
  (http) or to connect/handshake (tcp/tls) and is recorded whenever the target answered (also with a bad
  HTTP status); when it did not answer there is no latency, not a zero one. A result is one
  observation: no retries and no smoothing.
- **Scheduling.** One schedule per target, first run jittered within the first 10 s, at most 8 checks in
  flight, stopped cleanly on shutdown. Creating or deleting a target takes effect immediately.
- **State** (derived like host state, never stored): `UP` last result ok; `DOWN` last result failed (with its
  error text); `STALE` last result older than max(3 × interval, 90 s); `NEW` no result yet. A certificate with
  ≤ 14 days left is shown as text (`TLS 9d`), not as a state; alerting stays deferred.
- **API** (session required): `GET /api/uptime` → `{"targets":[{id,name,kind,target,interval_seconds,
  created_at,last,latency}]}` where `last` is the newest result or `null` and `latency` is
  `[[ts_ms, latency_ms|null], …]` for the last hour; `POST /api/uptime` → `201` target (`400` invalid,
  `409` duplicate name or limit reached); `DELETE /api/uptime/{id}` → `204`, results are deleted with it.

## 4f. Packaging, release and backup (Change 09)

- **Binary.** One static executable (`CGO_ENABLED=0`, `-trimpath`, stripped) for `linux/amd64` and
  `linux/arm64`, with the SPA embedded (the UI is built before `go build`) and the release SHA stamped by
  `-ldflags "-X main.release=<sha>"`. `smotryashchiy version` prints the release and the number of embedded
  migrations. A `SHA256SUMS` file accompanies the binaries.
- **Image.** Multi-stage build (node → go → `gcr.io/distroless/static-debian12:nonroot`): no shell, CA
  roots included (the prober needs them), user `65532`, works with a read-only root filesystem, writes only
  under `/data` (`SMOTRYASHCHIY_DB_PATH=/data/smotryashchiy.db`). Exposes `8080/tcp` and `51820/udp`. The image
  `HEALTHCHECK` runs `smotryashchiy healthcheck`, which requests `GET /health/ready` on the configured
  listen address (loopback for wildcard addresses) with a 3 s timeout and exits `0` only on `200`.
- **Compose example** `deploy/docker-compose.yml`: the server with a named volume, the two ports, a
  read-only rootfs and the production environment (§ STACK env table). Change 10 adds a built-in-ACME compose variant (§4g); until then, or if you prefer it, production
  mode expects a TLS-terminating reverse proxy (`SECURE_COOKIES`, `TRUSTED_PROXY_CIDRS`).
- **Backup.** `smotryashchiy admin backup --out FILE|-` writes a consistent snapshot of a **running** server's
  database (SQLite `VACUUM INTO`, no downtime) as a `.tar.gz` holding `smotryashchiy.db` and
  `manifest.json` (`created_at`, `release`, `migrations`, `db_sha256`). The bundle contains secrets (the
  admin password hash and the WireGuard server key): it is created with mode `0600` and never overwrites an
  existing file; `--out -` streams the bundle to stdout (refused on a terminal) and `--from -` reads it from
  stdin, so a bundle never has to cross a container/host file-permission boundary. `smotryashchiy admin
  restore --from FILE|- [--force]` verifies the manifest hash, that
  `PRAGMA integrity_check` is `ok` and that the bundle's migration count is not newer than the binary's, then
  atomically replaces the database. It refuses to touch an existing non-empty database without `--force`, and
  refuses while the database is locked by a running server. Off-host copying stays the operator's job.
- **Release Gate** (STACK.md): image builds, runs as non-root, becomes `healthy`, serves `/`, keeps its data
  across a container restart, and passes a vulnerability scan with no fixable HIGH/CRITICAL findings.

## 4g. Built-in TLS, release automation and the runbook (Change 10)

- **ACME.** Setting `SMOTRYASHCHIY_TLS_DOMAIN` switches the server into self-terminated TLS
  (`certmagic`, HTTP-01 challenge): it serves the app on `SMOTRYASHCHIY_HTTPS_ADDR` (default `:8443`)
  and, on `SMOTRYASHCHIY_ACME_HTTP_ADDR` (default `:8080`), the ACME challenge plus a redirect of
  everything else to `https://`. Certificates are stored under `<DB dir>/acme` (writable next to the
  database on the read-only image). `SMOTRYASHCHIY_ACME_EMAIL` is optional (renewal notices);
  `SMOTRYASHCHIY_ACME_CA=staging` selects Let's Encrypt's staging CA (untrusted certs, no rate limit)
  for testing a new domain before switching back to the default production CA. `/health/ready` is
  mounted on both listeners (unencrypted on the ACME port too), so `healthcheck` never depends on a
  certificate being ready yet. No low ports are bound in-process: operators map host `80`→container
  `8080` and host `443`→container `8443` (`deploy/docker-compose.acme.yml`), so the nonroot user needs
  no capability. **Production mode** now accepts either this (TLS domain set) or the Change 09 path
  (a trusted reverse proxy: `SECURE_COOKIES=true` and `TRUSTED_PROXY_CIDRS` set) — never neither.
- **Release automation.** `.github/workflows/release.yml`, triggered by a `v*` tag: builds the release
  binaries and `SHA256SUMS` (`scripts/build-release.sh`) and attaches them to a GitHub Release; builds a
  multi-arch (`amd64`+`arm64`) image, runs `scripts/image-smoke.sh` and `scripts/image-scan.sh` against
  it, and on PASS pushes `ghcr.io/<owner>/smotryashchiy:<tag>` and `:latest`. A release that fails smoke
  or scan is not published.
- **Runbook** (`docs/RUNBOOK.md`): first deploy with built-in ACME, a backup schedule example, routine
  checks, and playbooks for the failures operators actually hit (certificate not issued, agent can't
  reach the tunnel, restore refused because the server is still running, disk filling up, a lost admin
  password), plus upgrade/rollback. `docs/DEPLOY.md` keeps the Change 09 reverse-proxy quick start and
  now links here for ACME and incidents.

## 4a. Other interfaces

`/healthz` and `/health/ready` (exact release) exist since Change 01; UI/admin APIs are specified
in the change that introduces them.

## 5. UI

**One page, no navigation.** Opening `/` shows a single dashboard; separators are whitespace and
hairlines, never cards. It is the simplification of the predecessor's Dashboard / Sources /
Notifications / Source-detail structure (`docs/reference/`), which stays frozen as a design donor.
Alerts/Notifications is deferred with alerting (§7). The UPTIME ledger lists targets (state text, name and
target, latency with a sparkline, TLS days, age); targets are added through a dialog and removed with an
inline confirmation, no separate page.

Layout, top to bottom: command bar (`$ smotryashchiy`, live-connection text, `+ add host`, `logout`),
**STATUS** strip (hosts and freshness counts, average CPU/memory), **HOSTS** ledger, **UPTIME** ledger (targets), **EVENTS** log.
A host row shows name, freshness state, four sparklines with current values (CPU, memory, disk, network)
and last-seen age; activating it expands the row in place (one open at a time) to large charts, disks per
mount, network per interface, load, swap, checks and that host's events. The window is fixed at 1 hour;
there are no filters, tabs or theme switch. Login is not a page: the login form replaces the dashboard
whenever an API call answers `401`.

Rules: host state is derived from freshness only (`OK` ≤ 45 s since `last_seen_at`, `STALE` ≤ 5 min,
`OFFLINE` beyond, `NEW` when never seen) and is always textual; missing data renders `—`, never `0`;
no threshold colors (alerting is deferred). Design baseline: `docs/reference/DESIGN.md` tokens (near-black
canvas, one mono stack, 2 px radius, hairlines, no shadows), following `prefers-color-scheme` with dark as
the default. Every chart has a textual alternative; keyboard use and narrow widths (rows stack ≤ 900 px)
are first-class; WCAG 2.2 AA.

**Derived values** (computed in the UI from the §4c catalog): current values come from `latest=true`
and are marked `—` when the series has no sample in the last 5 minutes; sparklines and detail charts use
the last hour (`step=60` for the overview, raw 10 s for one expanded host); the disk sparkline follows the
mount with the highest `disk.used_percent`; network throughput is the sum over interfaces of
`Δcounter/Δt` between consecutive samples (a counter that decreases is a reset and yields no point for
that interval); uptime is `uptime.seconds` formatted `14d 06h`. Live frames from §4.6 update current
values and extend series tails; a full refresh runs every 60 s and after every reconnect. The connection
is shown as text (`live`, `reconnecting`, `offline`); reconnect backoff is 1 s doubling to 10 s, and after two
failed attempts a lost session (e.g. a server restart) shows the login form instead of waiting.

**Chart layout contract** (uPlot): legends, tick labels and tooltips never extend beyond the chart
container at any width; the page never scrolls horizontally; empty or single-point series show a text
state instead of a broken canvas.

**Stack** (confirmed in Change 06): Vite, React, TypeScript (strict), `@base-ui/react` for every
control it offers (Accordion, Dialog, Field/Input, Button, Tooltip), `uplot` for charts, CSS Modules,
npm. Vitest + Testing Library for unit tests; Playwriter drives the real browser checks.

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
| 4 | Uptime prober. **Alert rules, alert lifecycle and Telegram are deferred** (architect decision 2026-09-21); revisit after the MVP is dogfooded |
| 5 | Docker, logs, fail2ban in the agent |
| 6 | Distribution: image/binary, ACME, backup, deploy workflow, runbook |
| 7 | Dogfood on a fresh VPS for infraegev2; derive v2 backlog (incl. analytics) |

## 8. Open Questions

- Stage-2 spike: local proof is Change 04's first item; reliability across common VPS/NAT setups is
  confirmed at Stage 7, otherwise fall back to HTTPS push.
- Supported agent platforms: Linux amd64/arm64 for MVP (assumption).
