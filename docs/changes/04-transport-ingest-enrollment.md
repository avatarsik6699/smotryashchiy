# CHANGE 04 — Transport, Ingest and Enrollment

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `04` |
| Slug | `transport-ingest-enrollment` |
| Title | Transport, Ingest and Enrollment |
| Status | `active` |
| Branch | `feature/04-transport-ingest-enrollment` |

---

## Goal

Stage 2, part 1: prove and build the agent-to-server path. A userspace-WireGuard spike gates the
work; then host enrollment, the tunnel-only `POST /api/ingest` endpoint and a minimal agent
(`enroll`, `push-file`) let a real batch travel from an enrolled agent through the tunnel into
storage and the live stream. See `docs/SPEC.md` §4b. No collectors, agent run loop, UI, alerting.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Other
- [x] `T1` **Spike (gate):** in one Go test, run a userspace WireGuard server and client (`wireguard-go` + netstack, no root/kernel module), complete the handshake over localhost UDP and exchange an HTTP request through the tunnel; record the verdict in Implementation Notes. On failure STOP and ask the architect about the HTTPS-bearer fallback — _Depends on:_ —

### Data
- [x] `D1` Migration `0004_enrollment.sql`: `host_enrollments`, `host_peers` (unique public key and tunnel IP) — _Depends on:_ T1

### Backend
- [x] `B1` Config: `SMOTRYASHCHIY_WG_PORT`, `SMOTRYASHCHIY_TUNNEL_CIDR`, public endpoint (`SMOTRYASHCHIY_PUBLIC_ENDPOINT`) with fail-fast validation — _Depends on:_ T1
- [x] `B2` Enrollment domain/application: secret generation (hash-only storage, 1 h TTL, single use, atomic consume), tunnel address allocation, uniform `401` for bad secrets, `409` for a duplicate key — _Depends on:_ D1
- [x] `B3` Server tunnel: persistent server key in `settings`, WireGuard device in-process, peers re-added from storage at start and added live on enrollment, `AllowedIPs` = own `/32` — _Depends on:_ B1, B2
- [x] `B4` `POST /api/enroll` (public route, per-IP rate limit) and `admin host create` printing the one-time agent command — _Depends on:_ B2, B3
- [x] `B5` `POST /api/ingest` on the tunnel listener only: host from source tunnel IP, `Idempotency-Key`, 1 MiB body cap, per-host rate limit, calls `Service.Ingest` with the hub publisher (the same service instance as the read API) — _Depends on:_ B3
- [x] `B6` Agent: `agent enroll` (own keypair, `0600` config) and `agent push-file` sending a §4.1 batch through the tunnel — _Depends on:_ B4, B5
- [x] `B7` End-to-end contract tests: admin host → enroll → push through tunnel → read API and stream show the records; zero value survives; replay by key; unknown/foreign source address rejected; enrollment secret single-use/expired/wrong; ingest not reachable on the public listener — _Depends on:_ B6

### Other (docs)
- [x] `T2` Update `docs/STACK.md` (structure, env vars, dependencies), `docs/KNOWN_GOTCHAS.md`, and SPEC §8 wording after the spike — _Depends on:_ B7

---

## Files

### Create / modify
~~~
internal/platform/db/migrations/0004_enrollment.sql
internal/platform/config/
internal/telemetry/                (host peers, enrollment, ingest handler)
internal/transport/                (WireGuard device, tunnel listener)   [new]
internal/agent/                    (enroll, push-file)                   [new]
cmd/smotryashchiy/                 (server wiring, agent and admin subcommands)
go.mod / go.sum                    (wireguard-go; check with Context7)
docs/STACK.md, docs/KNOWN_GOTCHAS.md, docs/SPEC.md (§8 only)
~~~

### Do NOT touch
- Collectors, agent run loop, buffering/retry (Change 05)
- UI, alerting, uptime prober, ACME
- `internal/auth` internals (add a public route only through its existing exemption list)

---

## Contracts

See `docs/SPEC.md` §3–§4b and the Files list above.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md).

Change-specific smoke, after the Full Gate: start the server, `admin host create`, run
`agent enroll` and `agent push-file` from a second process, and confirm the record appears via
`GET /api/metrics` and on `/api/stream`; confirm `/api/ingest` on the public address is not served.
Running WireGuard may need `CAP_NET_BIND_SERVICE`-free high ports only; no root is expected.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Spike verdict (T1): PASS.** `TestSpikeHTTPOverUserspaceWireGuard` completes a WireGuard handshake
  over localhost UDP and serves/consumes HTTP through the tunnel with `wireguard-go` + gVisor
  netstack, as uid 1000, pure Go (no cgo, no kernel module, no `wireguard-tools`); the server saw
  the agent's tunnel address as the connection source, which is what B5 relies on. Not proven
  locally: reliability behind real NAT/firewalls (Stage 7 on a VPS).
- Enrollment lives in `internal/telemetry` (not a new bounded context) and `Store.CreateEnrollableHost`
  is separate from the older `Store.CreateHost` (no secret) used by tests.
- Ingest identity relies on WireGuard cryptokey routing (`AllowedIPs` = own /32): a peer cannot source
  another peer's address, so the source IP seen by the tunnel HTTP listener is trusted.
- `agent enroll` refuses to overwrite an existing config (it would destroy the private key);
  `/api/enroll` is rate limited by `RemoteAddr`, so behind a reverse proxy all clients share one
  bucket (10 burst, 1 per 6 s) until proxy-aware client IPs are reused there.
- `SMOTRYASHCHIY_PUBLIC_ENDPOINT` became mandatory in production config.
- Verified with the real binary in separate processes: `admin host create` -> `agent enroll` (config
  0600, second use of the secret 401) -> `agent push-file` (zero metric stored, replay reported) ->
  server restart restored the peer (`peers=1`) and a push still worked; public `/api/ingest` is 404.

---

## Commit Message

```
feat(change-04): tunnel, enrollment, tunnel-only ingest
```
