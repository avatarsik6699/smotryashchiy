# CHANGE 10 — ACME, Deploy Automation and Runbook

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `10` |
| Slug | `acme-deploy-runbook` |
| Title | ACME, Deploy Automation and Runbook |
| Status | `archived` |
| Branch | `feature/10-acme-deploy-runbook` |

---

## Goal

Stage 6, part 2 (final): built-in TLS via certmagic so the server can run with no separate reverse
proxy, a tag-triggered release workflow that builds, gates and publishes the image and binaries, and
an operator runbook. See `docs/SPEC.md` §4g. This closes Stage 6. No UI changes; the Change 09
reverse-proxy path stays supported.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Backend
- [x] `B1` Config: `TLSDomain`, `HTTPSAddr` (default `:8443`), `ACMEHTTPAddr` (default `:8080`), `ACMEEmail`, `ACMECA` (`production`|`staging`); production validation becomes "TLS domain set" OR "SecureCookies + TrustedProxyCIDRs set", never neither — _Depends on:_ —
- [x] `B2` `httpserver.Server` gains a TLS-serving mode: the app on the HTTPS listener, an ACME-challenge-plus-redirect handler on a second listener, `/health/ready` mounted on both, clean shutdown of both listeners — _Depends on:_ B1
- [x] `B3` Wire certmagic in `server.go` when `TLSDomain` is set: file storage under `<DB dir>/acme`, staging CA switch, email optional; `PublicURL` defaults to `https://<TLSDomain>` when unset — _Depends on:_ B2
- [x] `B4` `healthcheck` probes the ACME HTTP listener (never TLS) when TLS is enabled, so it never depends on a certificate being ready — _Depends on:_ B3
- [x] `B5` Contract tests: production validation branches, the ACME HTTP listener serves `/health/ready` and 301-redirects everything else, TLS listener serves the app once a cert is available (self-hosted CA/test server, no real ACME calls in tests) — _Depends on:_ B3, B4

### Infra
- [x] `I1` `deploy/docker-compose.acme.yml` (host `80`→container `8080`, host `443`→container `8443`, no reverse proxy, read-only rootfs) alongside the existing `docker-compose.yml` — _Depends on:_ B3
- [x] `I2` `.github/workflows/release.yml`: on tag `v*`, build binaries + `SHA256SUMS`, build the multi-arch image (buildx + qemu), run `image-smoke.sh` and `image-scan.sh` against it, and only on PASS create the GitHub Release with the binaries attached and push `ghcr.io/<owner>/smotryashchiy:<tag>` and `:latest` — _Depends on:_ I1

### Other
- [x] `T1` `docs/RUNBOOK.md` (first deploy with ACME, backup schedule, routine checks, failure playbooks, upgrade/rollback) and DEPLOY.md/STACK.md/KNOWN_GOTCHAS cross-links; real verification recorded in Implementation Notes: the ACME compose variant run locally against the Let's Encrypt **staging** CA with a real (or hosts-file) domain to prove the two-listener/redirect/health wiring, and the release workflow validated with `act` or a dry run of its steps where a live tag push is not appropriate — _Depends on:_ B5, I2

---

## Files

### Create / modify
~~~
internal/platform/config/          (TLSDomain, HTTPSAddr, ACMEHTTPAddr, ACMEEmail, ACMECA)
internal/platform/httpserver/      (TLS-serving mode)
cmd/smotryashchiy/server.go, ops.go
deploy/docker-compose.acme.yml     [new]
.github/workflows/release.yml      [new]
docs/RUNBOOK.md [new], docs/DEPLOY.md, docs/STACK.md, docs/KNOWN_GOTCHAS.md
go.mod / go.sum                    (certmagic; check with Context7)
~~~

### Do NOT touch
- UI, telemetry, agent, transport, uptime contracts
- Change 09's plain-HTTP-behind-proxy path (kept, not replaced)
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4g and STACK.md's env table.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md). The Release Gate (image) from Change 09 still applies.

Change-specific smoke, after the Full Gate: run the ACME compose variant against the Let's Encrypt
staging CA with a real domain (or `/etc/hosts` override) pointing at this machine, confirm the HTTP
port redirects to HTTPS, a staging certificate is issued and served, and `/health/ready` answers on
the ACME port throughout. If no domain is available for a live ACME exchange, this step is reported
as SKIPPED with the reason, and the non-ACME path is verified instead.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Architecture deviation from the plan, found empirically:** certmagic's embedded HTTP-01 solver
  always tries to bind its own listener (port 80 by default) for each challenge, regardless of an
  external `HTTPChallengeHandler`-wrapped listener already running. The fix is `ACMEIssuer.AltHTTPPort`
  set to our own ACME listener's port, so the embedded bind collides (`EADDRINUSE`) and certmagic falls
  back to the external listener correctly. Also, the external listener must be bound (`net.Listen`)
  *before* `ManageSync` is called, not concurrently with it — the ACME server validates by connecting to
  it. `httpserver.ServeTLS` (two listeners in one call) was replaced with `ServeHTTPS` (TLS listener
  only); the ACME HTTP-01 listener's lifecycle now lives in `cmd/smotryashchiy/acme.go`, matching the
  existing pattern for the tunnel ingest listener. See `docs/KNOWN_GOTCHAS.md`.
- **Real, live ACME verification (T1):** ran the actual committed `setupACME` code path against a real
  ACME server — [Pebble](https://github.com/letsencrypt/pebble) plus `pebble-challtestsrv` for DNS —
  entirely locally (three containers on one Docker network; Pebble configured with `httpPort: 8080` to
  match our `ACMEHTTPAddr`). To point the shipped code at Pebble instead of Let's Encrypt without adding
  an unplanned config surface, a temporary env-gated override block was added to a local working-tree
  copy of `acme.go` for this one verification run only, then reverted (`git diff` confirms `acme.go`
  matches the committed design with no leftover test hooks). Result: real ACME account registration,
  HTTP-01 challenge served and validated (`authz_status: valid`), certificate issued by Pebble's
  intermediate CA, TLS app listener served `200` over real TLS 1.3 with the issued certificate and the
  full CSP header set, and the ACME HTTP listener correctly redirected (`301`) and served `/health/ready`
  throughout. All test containers, volumes, network and images were removed afterward.
- **Release workflow (I2):** not run via `act` (unavailable in this environment) or a real tag push
  (would publish a real GHCR image/GitHub Release under the user's account without being asked).
  Instead, every step's underlying command was run directly: `scripts/build-release.sh`,
  `scripts/image-smoke.sh`, `scripts/image-scan.sh` (already covered by Change 09's Release Gate run),
  plus a full `docker buildx build --platform linux/amd64,linux/arm64` of the real `Dockerfile` (~4 min,
  no push), proving the exact build the workflow performs succeeds for both architectures. The `arm64`
  binary was executed under QEMU emulation this time (`docker run --platform linux/arm64 ... version`)
  and printed the correct release/migration string — real execution, though emulated rather than native
  arm64 hardware. Actions are pinned by commit SHA, matching `ci.yml`'s convention.
- Production's fail-closed rule changed from "reverse proxy always required" to "TLS domain OR reverse
  proxy, never neither" (`internal/platform/config`); Change 09's `docker-compose.yml` path is untouched
  and still works.

---

## Commit Message

```
feat(change-10): built-in ACME, release workflow, operator runbook
```
