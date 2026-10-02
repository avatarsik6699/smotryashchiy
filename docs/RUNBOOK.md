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

**Resource stalls with modest CPU and free disk space (Change 25).**
After updating the host agent, query the existing authenticated
`GET /api/metrics?host=<id>&name=pressure.io.some_percent&latest=true` (and
`pressure.io.full_percent`, `cpu.iowait_percent`, `cpu.steal_percent`). For a time series use
the same host/name filters with `from`/`to`; `step=60` requests minute buckets.
Use an existing operator session; never put its cookie/password in a URL or an incident report.
The new series are diagnostic API data; the existing dashboard has no new pressure charts or
thresholds. Inspect timestamps: `latest=true` can return old samples, and missing values from
an older agent or an unsupported PSI kernel mean unknown, not healthy/zero.

PSI `avg10` measures the share of time tasks are blocked over a ten-second window; `some` means
at least one task and `full` means all non-idle tasks. CPU `full` is deliberately omitted because
it is undefined at the system level. See [kernel PSI documentation](https://www.kernel.org/doc/html/latest/accounting/psi.html).
CPU iowait is not a reliable standalone measure; Linux can decrease that counter, in which case
the agent omits the interval. Correlate with PSI and kernel logs, not a single threshold.

For a suspected incident, record UTC intervals, agent freshness, resource samples, and bounded
`journalctl -k --since <start> --until <end>` / `journalctl -u systemd-journald` excerpts.
Blocked `jbd2` tasks and journald watchdogs support a guest-visible storage wait; they do not
by themselves identify a hypervisor/provider fault. Check available RAM, free space, scheduled
backup/restore jobs and device errors before escalating to the provider. Do not disable the
kernel warnings or spool fsync: a blocked disk can also stop the durable telemetry writer,
so a gap is not evidence of a quiet host. Do not run destructive disk tests on production.

**Availability coverage.**
Keep the homepage probe and add distinct HTTP targets for the application's `/health/ready`
and the monitoring server's `/health/ready` through the existing target UI/API when configuring
production monitoring. Verify the response and no-cache policy before relying on a readiness
URL; the application probe must exercise its database. Homepage success does not establish API
or database health. A check run by this monitoring server cannot independently detect its own
complete outage; an external observer is still needed for that case. Target creation is an
operator action, not an automatic side effect of an agent upgrade.

**Routine container messages marked as warnings.**
Update the agent on the emitting host, not just the server. Change 25 recognizes explicit
levels in supported log envelopes, including successful ACME maintenance and PostgreSQL `LOG`
records written to stderr. Unrecognized output still follows the stream fallback and is not
discarded. PostgreSQL `FATAL` and application `ERROR` remain visible errors. Historical stored
events retain their old classification. SSH/network errors and kernel stalls are not suppressed.

**Certificate was never issued (ACME mode).**
Check `docker compose logs server` for the `obtain` log lines. Common causes: the DNS record does not
point here yet (`dig`); port 80 is not actually reachable from the internet (test with
`curl http://monitor.example.com/healthz` from an *external* machine — a corporate/home firewall or an
ISP blocking inbound 80 will show up here); or `SMOTRYASHCHIY_TLS_DOMAIN` has a typo. Fix the cause and
`docker compose restart server`; there is no manual "retry" command, a restart re-attempts at start-up.

**Certificate exists, but ARI/renewal lock maintenance repeats an error.**
Check `docker compose logs --since 24h server` for `certmagic/locks` errors and the path in each
message. Both certificate assets and maintenance locks must be under the writable `/data/acme`
volume; `/home/nonroot/.local/share/certmagic/locks` means the running image still uses the old
default maintenance cache. Verify the image release and `/data` mount, then deploy a reviewed fix
through the normal release procedure. Do not make the root filesystem writable or copy certificate
material into the home directory. After release, verify `/health/ready`, HTTPS certificate dates
and absence of new lock errors over a maintenance interval; record the result without private keys.

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
v=v0.2.6; cd /tmp
curl -fsSLO "https://github.com/avatarsik6699/smotryashchiy/releases/download/$v/smotryashchiy-linux-amd64"
curl -fsSLO "https://github.com/avatarsik6699/smotryashchiy/releases/download/$v/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS
cp /usr/local/bin/smotryashchiy /usr/local/bin/smotryashchiy.bak   # rollback copy
install -m 755 smotryashchiy-linux-amd64 /usr/local/bin/smotryashchiy
systemctl restart smotryashchiy-agent && /usr/local/bin/smotryashchiy version
```

The host row must turn `OK` again within a minute. Agents before v0.2.6 do not report `cpu.count`,
so the dashboard cannot judge their load (it shows `load … CPU count unknown`); update them. Remove the `.bak` once the new agent has run
cleanly; restore it and restart the unit to roll back.

## Host hygiene

Hosts with UFW should run `ufw logging off`. With logging on, every blocked port-scan packet is a
kernel `[UFW BLOCK]` line, which the agent forwards as a `warn` event (hundreds per hour on a public
IP); real signals drown in it. fail2ban reads the sshd log, not UFW's, so bans are unaffected.
