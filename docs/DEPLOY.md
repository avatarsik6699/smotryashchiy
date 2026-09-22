# Deploying smotryashchiy

This is the first deployment guide (Change 09). Built-in TLS (ACME), the release workflow and the
complete operations runbook arrive with Change 10; until then the server expects a TLS-terminating
reverse proxy in front of it.

## What you run

One container (or one static binary) with three modes: `server` (what you deploy), `agent` (runs on each
monitored host) and `admin` (maintenance). The image is `distroless/static:nonroot` (about 28 MB), runs as
uid `65532`, works with a read-only root filesystem and writes only under `/data`.

Ports: `8080/tcp` (UI and API; put a TLS proxy in front) and `51820/udp` (the WireGuard tunnel agents use;
it must be reachable from your hosts).

## Quick start with Docker Compose

1. Copy `deploy/docker-compose.yml` and set the three settings for your domain:

   | Variable | Meaning |
   |----------|---------|
   | `SMOTRYASHCHIY_PUBLIC_URL` | `https://` URL of the UI, shown to agents in enrollment commands |
   | `SMOTRYASHCHIY_PUBLIC_ENDPOINT` | `host:port` agents dial for the tunnel (UDP `51820` by default) |
   | `SMOTRYASHCHIY_TRUSTED_PROXY_CIDRS` | your reverse proxy's address(es) as the container sees them |

   Production mode (`SMOTRYASHCHIY_PRODUCTION=true`) is fail-closed: it refuses to start with a missing
   setting or without an admin password, and names the setting in the log.

2. Set the admin password once (from stdin only, at least 24 bytes; it is never accepted as an argument):

   ```bash
   printf '%s\n' "$PASSWORD" | docker compose run --rm -T server admin set-password
   ```

3. Start it and check it:

   ```bash
   docker compose up -d
   docker compose ps          # STATUS shows (healthy) within ~15 s
   ```

4. Put a reverse proxy in front of `127.0.0.1:8080` (Caddy: `monitor.example.com { reverse_proxy 127.0.0.1:8080 }`).
   The session cookie is `Secure`, so the UI must be opened over HTTPS.

## Adding a host

Open the UI, choose **+ add host**, name it, and run the command it shows on that host (it is valid for one
hour and shown once). The agent binary comes from the release artifacts (`scripts/build-release.sh` builds
`smotryashchiy-linux-amd64` and `-arm64` with `SHA256SUMS`):

```bash
smotryashchiy agent enroll --server https://monitor.example.com --secret <shown once>
smotryashchiy agent run --interval 10s
```

The agent needs outbound UDP to `SMOTRYASHCHIY_PUBLIC_ENDPOINT` only; it opens no ports.

## Health

`GET /health/ready` answers `200` with the release SHA and `503` when the database cannot be reached. The
container `HEALTHCHECK` runs `smotryashchiy healthcheck`, which asks that endpoint on the local listen address.
`smotryashchiy version` prints the release SHA and the number of embedded migrations.

## Backup and restore

The database holds everything: hosts, telemetry, uptime targets, the admin password hash and the
WireGuard server key. A backup is a consistent snapshot of a **running** server, no downtime:

```bash
(umask 077; docker compose exec -T server /smotryashchiy admin backup --out - > backup-$(date +%F).tar.gz)
```

The bundle is a `.tar.gz` (`manifest.json` + `smotryashchiy.db`). **It contains secrets**: keep it private and
copy it off the host yourself (nothing leaves the machine automatically). Streaming through stdout avoids
file-permission problems between your user and the container's uid `65532`.

To restore, stop the server, then stream the bundle into the volume:

```bash
docker compose stop server
docker compose run --rm -T server admin restore --from - --force < backup-2026-09-22.tar.gz
docker compose start server
```

The restore verifies the bundle's hash, runs SQLite's integrity check and refuses a bundle from a newer
schema than the binary. `--force` is required to replace a non-empty database; the replaced file is kept
next to it as `smotryashchiy.db.pre-restore-<time>`. It refuses while a server still has the database open.
Because the WireGuard server key is restored too, agents reconnect on their own without re-enrolling.

## Upgrading

1. Take a backup (above).
2. `docker compose pull && docker compose up -d`. Migrations are forward-only and run at start-up.
3. Roll back by restoring the pre-upgrade bundle with the previous image (a binary older than the bundle's
   schema refuses it, by design).

## Release binaries and platforms

`bash scripts/build-release.sh` builds the UI and static `linux/amd64` and `linux/arm64` binaries with the
release SHA stamped in and writes `release/SHA256SUMS`. The arm64 binary is cross-compiled and verified
static, but it has not been executed on arm64 hardware by the project's checks.

## Security notes

- Keep `51820/udp` open only as needed; ingest is served only inside the tunnel, never on the public port.
- The image runs with a read-only root filesystem, dropped capabilities and `no-new-privileges` in the compose file.
- Backups and the admin password are the two secrets to protect. Rotate the password by running
  `admin set-password` again while the server is stopped.
