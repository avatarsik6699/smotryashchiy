# CHANGE 09 — Packaging and Backup

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `09` |
| Slug | `packaging-and-backup` |
| Title | Packaging and Backup |
| Status | `active` |
| Branch | `feature/09-packaging-and-backup` |

---

## Goal

Stage 6, part 1: make the product shippable. Static release binaries, a non-root distroless image with
a healthcheck and a compose example, `version` and `healthcheck` subcommands, an online backup and a
safe restore of the SQLite database, and a Release Gate that builds, runs and scans the image. See
`docs/SPEC.md` §4f. ACME, the deploy workflow and the full runbook are Change 10; no UI changes.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. -->

### Backend
- [x] `B1` `version` subcommand (release SHA, embedded migration count) and `healthcheck` subcommand (`GET /health/ready` on the configured address, wildcard → loopback, 3 s timeout, exit 0 only on 200) — _Depends on:_ —
- [x] `B2` `admin backup --out FILE`: consistent online snapshot via `VACUUM INTO`, `.tar.gz` with `smotryashchiy.db` + `manifest.json` (created_at, release, migrations, db_sha256), mode `0600`, never overwrites; tests including writes during the backup — _Depends on:_ B1
- [x] `B3` `admin restore --from FILE [--force]`: verify manifest hash, `integrity_check`, migration count not newer than the binary, refuse a non-empty database without `--force` and a database locked by a running server, atomic replace; round-trip test (hosts, metrics, targets, settings equal) — _Depends on:_ B2

### Infra
- [x] `I1` `scripts/build-release.sh`: UI build then static `linux/amd64` + `linux/arm64` binaries (`-trimpath`, stripped, release SHA stamped), `SHA256SUMS`; verify the binaries are static and the amd64 one runs `version` — _Depends on:_ B1
- [x] `I2` `Dockerfile` (node → go → distroless nonroot, healthcheck, OCI labels, `/data` volume, ports), `.dockerignore`, `deploy/docker-compose.yml` (named volume, read-only rootfs, production env) — _Depends on:_ I1
- [x] `I3` `scripts/image-smoke.sh`: build, run as non-root with a read-only rootfs, wait for `healthy`, `GET /` serves the UI, log in, restart the container and confirm the data survived, clean up; plus the vulnerability scan (pinned Trivy, fail on fixable HIGH/CRITICAL) — _Depends on:_ I2
- [x] `I4` Release Gate in `docs/STACK.md` (image build, smoke, scan) and a CI job running it; the fast/full gates stay Docker-free — _Depends on:_ I3

### Other
- [x] `T1` Docs: `docs/DEPLOY.md` (run with Docker, environment, health, backup and restore, upgrade, arm64 note), README status (Russian), STACK.md, KNOWN_GOTCHAS; real verification recorded in Implementation Notes: image built and run locally, a real agent enrolled against the container's mapped UDP port and visible on the dashboard (Playwriter), `docker stop`/`start` keeps data, backup taken while the container runs, volume wiped, restore, data back; the arm64 binary is only cross-compiled (its execution is reported SKIPPED) — _Depends on:_ B3, I4

---

## Files

### Create / modify
~~~
cmd/smotryashchiy/                (version, healthcheck, admin backup/restore)
internal/platform/backup/         [new]
scripts/build-release.sh, scripts/image-smoke.sh   [new]
Dockerfile, .dockerignore, deploy/docker-compose.yml   [new]
.github/workflows/ci.yml          (image job)
docs/DEPLOY.md [new], docs/STACK.md, docs/KNOWN_GOTCHAS.md, README.md
~~~

### Do NOT touch
- ACME/certmagic, the deploy workflow and the complete runbook (Change 10)
- UI, telemetry, agent, transport, uptime contracts
- `docs/reference/`

---

## Contracts

See `docs/SPEC.md` §4f and STACK.md's env table.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate in `/ship`. Both are defined in
> [docs/STACK.md](./STACK.md). The Release Gate (image) is defined by this change and runs in
> `/ship --release`; for this change it is also run once by hand.

Change-specific smoke, after the Full Gate: run `scripts/image-smoke.sh` (build, non-root, healthy, UI
served, data survives a restart), take a backup from the running container, restore it into an empty
volume and see the same hosts and targets.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- **Streaming backup/restore (found while verifying in Docker):** a `0600` bundle cannot be copied between host and
  container (uid 65532) as a file, so `admin backup --out -` streams to stdout and `admin restore --from -` reads
  stdin. `backup.Create` takes an `io.Writer` and a work directory (the database directory: writable on a read-only
  rootfs); `CreateFile` keeps the O_EXCL 0600 file behavior; `Restore` takes an `io.Reader`.
- **Restore safety:** an idle server connection also blocks a restore (proven by test with a pool connection and no
  transaction); a replaced database is kept as `.pre-restore-<time>`; work directories are always removed.
- **Real verification (T1):** release SHA `f5c818e` binaries built (`amd64` 15.4 MB, `arm64` 14.7 MB, both static,
  `SHA256SUMS`); image 28 MB, digest-pinned bases, Trivy 0.74.0: 0 vulnerabilities (Debian 12 and the Go binary).
  `scripts/image-smoke.sh` passes all steps (uid 65532, read-only rootfs, healthy, UI + CSP, data survives a
  restart, streamed backup mode 600 and restore, production boot healthy, incomplete production config refused with
  the setting named). A real agent (release binary) enrolled against the container through the mapped ports and
  appeared on the dashboard as `OK` (Playwriter, dark, 1280 px). `docker stop/start`: peers restored, agent
  reconnected. **Backup of the running container, then container and volume destroyed, brand-new volume, restore:**
  `peers=1`, the same admin password works, the host and its history are back, and the agent reconnected without
  re-enrolling (the WireGuard server key was restored with the database); the dashboard showed it `OK` again.
- **Not run:** the `arm64` binary (cross-compiled, verified static only), a multi-arch image (`docker buildx` +
  qemu is not part of this change), and the CI image job (it runs the same scripts on GitHub; not executed here).
- The compose file and DEPLOY.md assume a TLS-terminating reverse proxy until ACME (Change 10).

---

## Commit Message

```
feat(change-09): static release binaries, distroless image, backup and restore
```
