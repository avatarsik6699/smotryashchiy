# Operator runbook

This complements `docs/DEPLOY.md` (which covers the reverse-proxy quick start): first deploy with
built-in TLS, routine operations, and what to do when something goes wrong. Read `docs/DEPLOY.md`
first if you have not deployed yet.

## First deploy with built-in TLS (no reverse proxy)

Use `deploy/docker-compose.acme.yml` instead of `deploy/docker-compose.yml` when you would rather the
server terminate HTTPS itself than run a separate proxy.

1. Point `monitor.example.com`'s DNS A/AAAA record at this host. Confirm it resolves:
   `dig +short monitor.example.com` should print this host's public IP.
2. Make sure ports 80 and 443 are free on the host (nothing else is bound to them). The container
   itself never needs root or a capability: it listens on unprivileged 8080/8443, and Docker maps the
   host's 80/443 to them.
3. Edit `SMOTRYASHCHIY_TLS_DOMAIN`, `SMOTRYASHCHIY_ACME_EMAIL` and `SMOTRYASHCHIY_PUBLIC_ENDPOINT` in the
   compose file for your domain. For a domain you have not issued a certificate for before, uncomment
   `SMOTRYASHCHIY_ACME_CA: staging` first — Let's Encrypt's staging CA has no rate limit, but its
   certificates are not trusted by browsers, so switch it back (remove the line) once you are ready for
   a real certificate.
4. Set the admin password (stdin only) and start:
   ```bash
   printf '%s\n' "$PASSWORD" | docker compose -f docker-compose.acme.yml run --rm -T server admin set-password
   docker compose -f docker-compose.acme.yml up -d
   ```
5. Watch it come up: `docker compose -f docker-compose.acme.yml logs -f server` shows the ACME exchange
   (`obtaining certificate` → `certificate obtained successfully`) and then `server started`. This can
   take up to a minute — Let's Encrypt itself, not this software, sets the pace. `docker compose ps`
   should report `(healthy)`.
6. Open `https://monitor.example.com/`. If you used the staging CA, your browser will warn about the
   untrusted certificate — expected; switch to the production CA (step 3) and restart once you have
   confirmed the domain and port-forwarding work.

## Backup schedule

Automate the manual backup command from `docs/DEPLOY.md` with cron on the Docker host (adjust the
compose file path):

```cron
# /etc/cron.d/smotryashchiy-backup — daily at 03:17, keep 14 days
17 3 * * * root umask 077; docker compose -f /opt/smotryashchiy/docker-compose.yml exec -T server \
  /smotryashchiy admin backup --out - > /var/backups/smotryashchiy/backup-$(date +\%F).tar.gz \
  && find /var/backups/smotryashchiy -name 'backup-*.tar.gz' -mtime +14 -delete
```

The bundle holds secrets (the admin password hash and the WireGuard server key): `/var/backups` should
be readable only by root, and copied off this host to be a real backup (this cron job alone protects
against database corruption, not host loss).

## Routine checks

- **Health**: `curl -f https://monitor.example.com/health/ready` (or the ACME-mode port, see
  `docs/DEPLOY.md`) — `200` with the running release SHA, `503` when the database cannot be reached.
- **Logs**: `docker compose logs --since 1h server`. Every request is logged with a `request_id`;
  correlate it with the `X-Request-Id` response header when investigating a specific failure. Info
  records (the access line among them) go to stdout, warnings and errors to stderr, so an agent
  watching the server container reports routine requests as `info` (docs/SPEC.md §4h).
- **Disk**: the SQLite file plus its `-wal`/`-shm` siblings under the `smotryashchiy-data` volume. Raw
  telemetry and uptime results are purged after `SMOTRYASHCHIY_RAW_RETENTION_DAYS` (default 30); rollups
  after `SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS` (default 396). If disk use is unexpectedly high, check
  `docker exec <container> /smotryashchiy admin backup --out - | wc -c` (the compressed database size)
  before lowering retention.
- **Agents**: the dashboard's host rows show `STALE`/`OFFLINE` in text when an agent stops reporting;
  it never silently reads as healthy.

## Failure playbooks

**Certificate was never issued (ACME mode).**
Check `docker compose logs server` for the `obtain` log lines. Common causes: the DNS record does not
point here yet (`dig`); port 80 is not actually reachable from the internet (test with
`curl http://monitor.example.com/healthz` from an *external* machine — a corporate/home firewall or an
ISP blocking inbound 80 will show up here); or `SMOTRYASHCHIY_TLS_DOMAIN` has a typo. Fix the cause and
`docker compose restart server`; there is no manual "retry" command, a restart re-attempts at start-up.

**An agent enrolled but never shows data.**
The agent needs outbound UDP to `SMOTRYASHCHIY_PUBLIC_ENDPOINT`; a firewall between the agent and this
host that blocks UDP (not just TCP) is the usual cause. Confirm the server's WireGuard port is open:
`docker compose logs server | grep 'tunnel started'` shows the UDP port and current peer count; it
should increase after a successful enrollment. On the agent side, `agent run` logs a warning when
batches cannot be delivered.

**Restore is refused with "already in use" / "requires --force".**
This is deliberate (`docs/DEPLOY.md`): a restore never overwrites data silently. If you mean to replace
the current database, add `--force` (the old file is kept alongside it, never deleted). If instead the
restore is refused for being "in use", the server is still running against that database — stop it
first (`docker compose stop server`).

**Lost the admin password.**
There is no recovery of the password itself (it is stored only as a bcrypt hash). Stop the server and
run `admin set-password` again with a new one — this only replaces the password hash, no other data is
touched, and does not require the old password:
```bash
docker compose stop server
printf '%s\n' "$NEW_PASSWORD" | docker compose run --rm -T server admin set-password
docker compose start server
```

**Disk filling up faster than expected.**
Lower `SMOTRYASHCHIY_RAW_RETENTION_DAYS`/`SMOTRYASHCHIY_ROLLUP_RETENTION_DAYS` (restart to apply); purges
run at start-up and then on their own schedule, so growth stops within one purge cycle, not instantly.

## Upgrade and rollback

See `docs/DEPLOY.md#upgrading`: back up, `docker compose pull && up -d` (migrations are forward-only and
run automatically), roll back by restoring the pre-upgrade bundle with the previous image tag — a binary
older than a bundle's schema refuses to restore it, by design, so always roll the image back together
with the data.

### Updating an agent

The agent is the same binary as the server. On each monitored host:

```bash
v=v0.2.5; cd /tmp
curl -fsSLO "https://github.com/avatarsik6699/smotryashchiy/releases/download/$v/smotryashchiy-linux-amd64"
curl -fsSLO "https://github.com/avatarsik6699/smotryashchiy/releases/download/$v/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS
cp /usr/local/bin/smotryashchiy /usr/local/bin/smotryashchiy.bak   # rollback copy
install -m 755 smotryashchiy-linux-amd64 /usr/local/bin/smotryashchiy
systemctl restart smotryashchiy-agent && /usr/local/bin/smotryashchiy version
```

The host row must turn `OK` again within a minute. Remove the `.bak` once the new agent has run
cleanly; restore it and restart the unit to roll back.

## Host hygiene

Hosts with UFW should run `ufw logging off`. With logging on, every blocked port-scan packet is a
kernel `[UFW BLOCK]` line, which the agent forwards as a `warn` event (hundreds per hour on a public
IP); real signals drown in it. fail2ban reads the sshd log, not UFW's, so bans are unaffected.
