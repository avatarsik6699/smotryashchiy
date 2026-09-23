# Known Gotchas

> Project memory file. Capture recurring pitfalls that repeatedly waste time during coding,
> testing, or deploys. Add only issues likely to happen again; prefer concrete symptoms, root
> cause and the shortest reliable fix; remove entries that stop being relevant.

## Gotcha Log

### Producer-side lessons inherited from the predecessor (sre-kit)

Carry these into the first contract tests instead of rediscovering them in production:

- **Zero is a value.** A metric of `0` (idle container CPU) must be emitted and stored; never let
  "omit if zero" serialization drop it, and never let a Metric-only field leak into Check records.
- **Timestamps come from the producer** and are validated: reject observations more than five
  minutes in the future; replayed overlapping records are stored once, not re-alerted.
- **Unknown must stay unknown.** No data, stale data and healthy data are three distinct UI states.

### SQLite `INSERT OR IGNORE` swallows constraint violations other than duplicates

- **Symptoms**: a record that violates a `CHECK` or `NOT NULL` constraint is silently skipped and
  counted as a "duplicate"; no error, no data.
- **Root cause**: `OR IGNORE` applies to every constraint class, not only uniqueness.
- **Fix**: dedupe with `INSERT ... ON CONFLICT DO NOTHING`, which only skips uniqueness conflicts
  and still fails loudly on `CHECK`/`NOT NULL`. Covered by
  `TestStoreFailureMidBatchRollsBackEverything`.

### Hijacked WebSocket connections outlive `http.Server.Shutdown`

- **Symptoms**: graceful shutdown waits out its timeout, or stream clients stay connected after the
  server is told to stop.
- **Root cause**: `Shutdown` does not track hijacked connections, and `websocket.Accept` hijacks.
- **Fix**: `telemetry/application.Hub.Close()` is called before `srv.Shutdown`; every stream handler
  exits when its subscription channel closes. New long-lived handlers must do the same.

### Raw purge is gated on rollups and the 24 h re-aggregation window

- **Symptoms**: raw rows older than the TTL are still present; or, if the gate were removed, a
  rollup for a late-data hour would be recomputed from partially purged raw data and shrink.
- **Root cause**: `Maintenance.Purge` deletes raw rows only before `min(now - TTL, rolled_through -
  24h)` and does nothing raw until the first rollup finished. A permanently failing rollup therefore
  stops raw purging (logged as `telemetry maintenance job failed`).
- **Fix**: fix the rollup error; do not loosen the gate.

### `INSERT ... SELECT ... ON CONFLICT` needs a `WHERE`

- **Symptoms**: SQL parse ambiguity when an upsert takes its rows from a `SELECT`.
- **Fix**: keep an explicit `WHERE` (`RollupHour` has one) on any `INSERT ... SELECT ... ON CONFLICT`.

### Two WireGuard handshakes from one key within ~20 ms stall for 5 s

- **Symptoms**: a fresh tunnel from an already-known peer key occasionally takes exactly ~5 s to
  connect (17 % of back-to-back pushes in a loop); the verbose log shows
  `ConsumeMessageInitiation: handshake replay` on the server.
- **Root cause**: `wireguard-go` rounds handshake timestamps to ~16.7 ms (anti-fingerprinting) and
  rate-limits initiations per peer to one per 20 ms. A second initiation inside that window is
  treated as a replay and only retried after the 5 s rekey timeout.
- **Fix**: none needed for a long-lived agent tunnel. Code or tests that create fresh tunnels for the
  same key back to back must space them apart (tests use `handshakeSpacing` = 40 ms).
  `transport.NewClient` waits for the first handshake so failures are fast and explicit.

### `pkill -f` / `pgrep -f` in verification scripts can kill the calling shell

- **Symptoms**: a smoke script dies with exit 144 or the "restarted" server is still the old one.
- **Fix**: start the server with `& echo $! > pid` and `kill $(cat pid)`; never match on a command
  line that also appears in the script itself.

### WSL/VM stalls of ~3 s look like agent or network bugs

- **Symptoms**: in a real-process verification, metric timestamps show a gap of 8.3 s instead of 5 s
  (two or three times per run), always the same size.
- **Root cause**: the environment freezes for ~3.3 s (a 1 s `sleep` in a watchdog took 4.34 s at the
  same moments); the agent's ticker simply lost those seconds.
- **Fix**: none in code. Before investigating a timing gap, run a `while true; do date +%s.%N; sleep 1`
  watchdog beside the experiment and compare.

### Agent "memory used" intentionally differs from `free`

- `memory.used_bytes` = `MemTotal - MemAvailable` (what applications cannot get without swapping),
  while `free` prints `total - free - buff/cache`. Compare against `MemAvailable`, not `free`'s used column.

### Other browser extensions pollute the console of a strict-CSP page

- **Symptoms**: in the user's real Chrome (via Playwriter) the console shows `Applying inline style
  violates ... 'style-src 'self''` and `p is not a function`, and the DOM contains dozens of foreign
  `<style>` elements (`.spantree-*`), although the app itself has no violation.
- **Root cause**: third-party extensions inject styles/scripts into every page; our CSP blocks them.
- **Fix**: none in code. Judge the app's console in a clean profile (Playwright MCP launches Chrome with
  `--disable-extensions`, an allowed fallback), and confirm with a `securitypolicyviolation` listener.

### Base UI `Field.Root invalid` blocks resubmitting a form

- **Symptoms**: after a wrong password the login form ignores the next submit.
- **Root cause**: Base UI treats a form with an invalid field as invalid and skips `onFormSubmit`; the
  error was only cleared inside the submit handler, a deadlock.
- **Fix**: convey server-side errors with `aria-invalid` and text, never with the `invalid` prop
  (`Login.tsx`, covered by `clears the previous error when submitting again`).

### A logged-out page load must not leave a red 401 in the console

- **Symptoms**: probing the session with an authenticated endpoint logs `Failed to load resource ... 401`.
- **Fix**: the SPA asks `GET /api/auth/session`, which always answers 200. The only 401 left in a
  normal flow is a genuinely wrong password.

### Playwright MCP runs Chrome on Windows

- **Symptoms**: `browser_take_screenshot` with a relative `filename` fails with `ENOENT C:\home\...`.
- **Fix**: omit `filename`; files land in `C:\Users\user\AppData\Local\Temp\.playwright-mcp`
  (`/mnt/c/Users/user/AppData/Local/Temp/.playwright-mcp` from WSL). Playwriter, driven from WSL,
  accepts WSL absolute paths.

### A directory ignore rule beats a later `!` exception

- **Symptoms**: `web/dist/.gitkeep` was not tracked although `.gitignore` had `!web/dist/.gitkeep`; a fresh
  clone failed `go build` (`go:embed all:dist` matches nothing).
- **Root cause**: an earlier `web/dist/` line ignored the whole directory, and git cannot re-include a file
  whose parent directory is ignored.
- **Fix**: ignore the contents (`web/dist/*`) and add `!web/dist/.gitkeep`. Always verify with
  `git clone . /tmp/x && cd /tmp/x && go build ./...` after touching embed inputs.

### `vi.unstubAllGlobals()` also removes the global stubs from the test setup

- **Symptoms**: every component test after the first one crashes (`matchMedia is not a function`).
- **Fix**: `web/src/test/setup.ts` assigns `ResizeObserver` and `matchMedia` directly (not with `vi.stubGlobal`),
  so tests may call `vi.unstubAllGlobals()` freely.

### A grid track sized by content lets a canvas widen the whole page

- **Symptoms**: horizontal page scroll on narrow screens although every chart "fits" its container.
- **Fix**: every grid that holds charts uses `minmax(0, 1fr)` tracks and `min-width: 0` on items; the chart box
  has `overflow: hidden` (SPEC §5 chart layout contract). Never `max-width: 100%` inside an auto track.

### Playwright/CDP `setOffline` does not drop an open WebSocket

- **Symptoms**: "offline" emulation leaves the stream `live`; reconnect behavior cannot be exercised.
- **Fix**: put a TCP proxy between browser and server that can destroy connections and refuse new ones while
  the session stays valid (the throwaway `proxy.mjs` used for Change 07 verification).

### Restarting the server logs every browser out (sessions are in memory)

- **Symptoms**: after a restart the WebSocket upgrade is refused with 401, which the browser reports like any
  other failure, so the page sat in "offline" until the next 60 s refresh.
- **Fix**: after two failed reconnects `StreamClient` asks `GET /api/auth/session`; a "not authenticated" answer
  shows the login form within ~1 s. Agents are unaffected (they use the tunnel, not sessions).

### Base UI 1.8 accordion: rows are tab stops, arrow keys do not move focus

- **Symptoms**: a keyboard test expecting ArrowDown to move between rows fails.
- **Fix**: the `loopFocus`/`orientation` props are deprecated no-ops; navigate with Tab, toggle with Enter/Space.

### A probe error must say what happened, and "no answer" must not become "0 ms"

- **Symptoms**: a DOWN target shown with a latency of 0, or an error text like `Get "http://...": dial tcp ...`.
- **Rules kept by the code and tests**: latency is `null` unless the target answered (an HTTP 500 did answer, so it
  keeps its real latency); the `*url.Error` wrapper is stripped; a timeout reads `timeout after 10s`; certificates are
  always verified (an untrusted or expired one is a failed check with the x509 message).

### Closed loopback ports in this WSL setup may time out instead of refusing

- **Symptoms**: a TCP probe of `127.0.0.1:<never-used port>` reports `timeout after 10s`, while a port whose
  listener was just stopped reports `connection refused` at once.
- **Fix**: none needed (both are correct DOWN results); do not write tests or scripts that expect an instant
  refusal for an arbitrary unused port. Unit tests close a listener they opened to get a refused connection.

### The canvas is briefly wider than its container after a window shrink

- **Symptoms**: for ~2 frames after the viewport shrinks, `uplot` elements measure wider than the chart box.
- **Why it is fine**: the box has `overflow: hidden`, so nothing is visible outside it; the ResizeObserver callback
  calls `setSize` synchronously to keep the window this short. Layout checks must sample ≥ 200 ms after a resize.

### A 0600 bundle cannot cross the host/container boundary as a file

- **Symptoms**: `docker cp`/bind-mounting a backup makes it unreadable for uid 65532 (or for you), because the
  bundle is `0600` and owned by whoever created it.
- **Fix**: stream it. `admin backup --out -` writes to stdout and `admin restore --from -` reads stdin (both refuse
  to touch a terminal / need explicit `--force`), so no file permissions are involved. Redirect with `umask 077`.

### The distroless image has no shell: use the binary itself

- `docker exec <c> /smotryashchiy <command>` works (`version`, `healthcheck`, `admin backup ...`); `docker exec <c> sh`
  does not. Work directories for backups live next to the database (`/data`) because the root filesystem is read-only.
- `docker top <c> -eo user,pid` needs the `pid` column; `-eo user` alone fails with "Couldn't find PID field".
- Production mode without an admin password refuses to start; create the container (`docker create`), run
  `admin set-password` in the same volume, then start it.

### Loopback ports and `docker run -p`

- A UDP port mapped for the tunnel (`-p 51820:51820/udp`) must be free on the host; an agent enrolled against a
  dev-mode server dials `127.0.0.1:<udp port>`, so test agents only work when host and container ports match.

### certmagic's embedded HTTP-01 solver tries to bind port 80 itself, even with your own listener already up

- **Symptoms**: `could not start listener for challenge server at :80: listen tcp :80: bind: permission
  denied`, even though your own server already has an ACME-HTTP-01 listener running (wrapped with
  `issuer.HTTPChallengeHandler`) on a different port.
- **Root cause**: certmagic's default ("embedded") solver always tries to bind its own listener on port
  80 (or `AltHTTPPort` if set) for the duration of each challenge. It only falls back to an externally
  owned listener (yours) when that bind fails with "address already in use" — nothing about wrapping your
  mux with `HTTPChallengeHandler` alone suppresses the embedded attempt.
- **Fix**: set `ACMEIssuer.AltHTTPPort` to the exact port your own listener already occupies, so
  certmagic's bind attempt collides with yours (`EADDRINUSE`) and it falls back correctly, instead of
  leaving the default (port 80, which an unprivileged/capability-less process can never bind). Also
  start your own listener (`net.Listen`, synchronously) **before** calling `ManageSync`: the ACME server
  validates by connecting to it, so it must already be accepting when the challenge is issued
  (`cmd/smotryashchiy/acme.go`).

### certmagic's `Resolver` override applies to every DNS lookup the ACME client makes, not just the challenge domain

- **Symptoms**: while testing against a local ACME server (Pebble) with a custom DNS resolver, the ACME
  client's own request to the CA's directory URL fails to connect — it resolved the *CA's* hostname
  through the test resolver too, which answered with the test domain's fake IP instead of the CA's real
  one.
- **Fix**: only the ACME *server* needs a resolver override that answers your test domain (Pebble's own
  `-dnsserver` flag); the *client* (our binary) should resolve the CA's own hostname normally. Verified
  with a full local exchange (server + Pebble + pebble-challtestsrv), see
  `docs/changes/archive/10-acme-deploy-runbook.md` Implementation Notes.

### A tailed collector's events need their own timestamp, not the batch's tick time

- **Symptoms**: two genuinely distinct log lines (e.g. two identical warning messages a few seconds
  apart) silently collapse into one stored row.
- **Root cause**: the agent's wire batch stamps every record with one `ts` per tick (`BuildBatch`).
  That is correct for a *snapshot* source (CPU, memory — "the value right now"), but a tailing
  source (journald, Docker container logs, fail2ban) can buffer several lines within one tick; if
  they all get the same tick `ts`, two lines with identical `level`+`message`+`labels` collide under
  the server's event identity `(host, ts, level, message, canonical labels)` and the second is
  stored-once as a duplicate.
- **Fix**: `collect.Event` carries an optional `TS`; a tailing collector sets it from the source's
  own timestamp (journald's `__REALTIME_TIMESTAMP`) and `BuildBatch` uses it instead of the tick
  time when non-zero (`internal/agent/batch.go`, Change 11). A snapshot-style check/event can leave
  `TS` zero and get the tick time, same as before.

### fail2ban jail names routinely contain hyphens, which the metric/check name regex forbids

- **Symptoms**: `server permanently rejected a batch; dropping it ... checks[0].name: must match
  ^[a-z][a-z0-9_.]{0,127}$` — found only by deploying against a real production host (Change 11's
  Stage-7 target VPS), not in local testing, because the local dev jail was named plainly (`sshd`).
- **Root cause**: fail2ban jail names are free-form and hyphens are idiomatic
  (`infraege-nginx-limit`, `nginx-limit-req`, …), but the server's metric/check `name` field only
  allows `[a-z0-9_.]` (docs/SPEC.md §4.1) — hyphens are not in that set.
- **Fix**: sanitize the jail name into the check name (`checkNameSafe`,
  `internal/agent/collect/fail2ban.go`: lowercase, any character outside `[a-z0-9_.]` becomes `_`)
  and keep the original jail name in `Meta.jail` for display. Any collector deriving a metric/check
  *name* from free-form host data (not just fail2ban) needs the same sanitization — labels have no
  such restriction and don't need it.

### Docker's multiplexed log stream frames don't align with line boundaries

- **Symptoms**: naively splitting each `/containers/{id}/logs?follow=true` read on newlines
  produces truncated or merged lines when a line's bytes straddle two TCP reads.
- **Root cause**: the stream is framed at the byte level (8-byte header: stream type, 3 zero bytes,
  big-endian `uint32` payload size — https://docs.docker.com/reference/api/engine/ — "Stream
  format"), completely independent of where the underlying process's `\n`s fall; one frame can end
  mid-line.
- **Fix**: demux by the frame header first (`io.ReadFull` for the 8 bytes, then for the declared
  payload size), then split *that* payload on `\n` while carrying the trailing partial line forward
  per stream (stdout and stderr buffered separately) to prepend on the next frame
  (`internal/agent/collect/docker_logs.go`'s `demuxDockerLogStream`).

### A history call is not a pageview: client routers call `replaceState` with the same URL

- **Symptoms**: a tracked SPA records two pageviews per page load. Found live on infraege.ru
  (TanStack Router): one `GET /track.js`, two `POST /api/collect`.
- **Root cause**: the router calls `history.replaceState` once during hydration without changing
  the URL. Routers also call it for scroll and state bookkeeping, and some pass no URL at all. A
  snippet that sends on every `pushState`/`replaceState` counts each of these calls.
- **Fix**: `track.js` sends only when `pathname + search` differs from the last URL it sent
  (Change 16). `track_test.go` runs the served snippet under Node and covers same-URL,
  state-only and hash-only calls.
- **Verify on a real page**: count network requests, not DOM nodes. TanStack Router's head
  `Script` asset removes its `<script>` node right after hydration, after the deferred script has
  already run. A DOM query for the tag therefore reports it missing even though tracking works.

### A read-only rootfs leaves SQLite no temp dir: `disk I/O error (6410)` once data grows

- **Symptoms**: `telemetry maintenance job failed job=rollup ... disk I/O error (6410)` in
  production. `rollup_state` stopped advancing (2026-09-22 11:00 UTC) while ingest and the UI kept
  working. The fault is latent: early hours rolled up fine.
- **Root cause**: 6410 is `SQLITE_IOERR_GETTEMPPATH`. SQLite spills large sorts, temporary B-trees
  and statement journals to a temp file. It looks in `SQLITE_TMPDIR`, `TMPDIR`, `/var/tmp`,
  `/usr/tmp`, `/tmp` and `.`, and with `read_only: true` none of them is writable. Small workloads
  stay in memory, so nothing fails until an hour's data outgrows the buffers.
- **Fix**: `temp_store=MEMORY` in `db.Open`'s DSN (Change 17). Do not add a `tmpfs` to one compose
  file instead: every read-only deployment needs the fix.
- **Reproduce**: create a DB with the real migrations and ~100k metric rows (300 series) in one past
  hour. Run the image with `--read-only --cap-drop ALL -v <dir>:/data`. Without the fix, the
  startup rollup logs 6410. With a writable rootfs, or with the fix, it writes 300 rollup rows.

### A tracked site's own test runs post real beacons; Lighthouse passes the bot filter

- **Symptoms**: pageviews stored for a site before it was ever deployed with the snippet. On
  infraege.ru, 72 of the first 74 rows came from local dev-server checks and the infraegev2 Full
  Gate. Lighthouse showed up as bursts of three hits per audited route from "Chrome / Android /
  mobile".
- **Root cause**: the snippet is part of the site's build, so every local, CI and Lighthouse run
  loads it and `sendBeacon`s to the production collector. A text/plain beacon needs no CORS
  preflight, so it is stored whatever its origin. Lighthouse sends a normal mobile Chrome
  User-Agent, so the bot list cannot catch it.
- **Fix**: `Collect` stores a beacon only when the `Origin` hostname is the site's domain or `www.`
  plus it (Change 18). Browsers send `Origin` on cross-origin POST, including `sendBeacon`
  (verified on infraege.ru: `Origin: https://infraege.ru`). A page with
  `Referrer-Policy: no-referrer` sends `Origin: null` and is therefore not tracked.

### Docker-owned files break host operations (`EACCES` / `EPERM` / read-only)

- **Symptoms**: file operations fail with `EACCES`, `EPERM`, "Permission denied" or
  "Read-only file system" on container-generated paths inside the repo.
- **Root cause**: a container wrote to a bind-mounted host directory as root.
- **Agent protocol**: never `sudo`, `chmod -R 777` or loop. Stop and post:

  > ⛔ **Permission denied.** I cannot modify `<path>` while running `<cmd>`. Please run
  > `sudo chown -R $USER:$USER <path>` (or `sudo rm -rf <path>` to discard the artefact), then reply
  > **`continue`** and I will retry from the same step.

  Retry once on `continue`; if the same error repeats, stop and ask the user to confirm the fix.
- **Prevention**: run containers with a matching UID/GID or use named volumes.

### `SameSite=Lax` does not stop a same-site subdomain from writing

- **Symptoms**: a page on another subdomain of the UI's registrable domain (`evil.infraege.ru` for a
  UI on `sre.infraege.ru`) creates uptime targets or sites through the operator's session.
- **Root cause**: "site" means registrable domain, so every subdomain is same-site and gets the Lax
  cookie on a POST; a `text/plain` body is a CORS-simple request, so no preflight stops it; and Go's
  `json.Decoder` does not care about `Content-Type`.
- **Fix**: `authhttp.GuardCrossOriginWrites` (docs/SPEC.md §4d) refuses unsafe-method `/api/*` writes
  from any other origin and requires `application/json`. A new public write route must be added to
  its `crossOriginExempt` list deliberately, like `/api/collect`.

### Docker's `journald` log driver duplicates container lines into the journal

- **Symptoms**: every container log line shows up twice in EVENTS, once labeled `container=…` and once
  `unit=docker.service`, often with different levels.
- **Root cause**: with `"log-driver": "journald"` Docker writes container output to the journal (with a
  `CONTAINER_NAME` field), and the Docker-logs source reads the same lines through the Docker API.
- **Fix**: the journald source skips entries carrying `CONTAINER_NAME` (docs/SPEC.md §4h). Check a host's
  driver with `docker info --format '{{.LoggingDriver}}'`.

### Docker `stats?stream=false` blocks ~2 s per container

- **Symptoms**: `agent run --interval 10s` delivers every ~18 s on a host with 9 containers.
- **Root cause**: a one-shot stats call waits for a second CPU sample; read one by one they add up, and
  Go's ticker drops ticks that fire while a tick is still collecting.
- **Fix**: the Docker collector reads stats concurrently under a per-tick deadline (docs/SPEC.md §4h).
  Measured on infraege.ru: 17.8 s serial vs 2.0 s parallel for 9 containers.

### `Server.Use` runs middleware in registration order — and a hand-built test chain hid it

- **Symptoms**: on v0.2.3 a cookieless cross-origin write answered `401` (session gating) instead of
  `403` (the cross-origin guard) in production, while the tests said `403`.
- **Root cause**: `httpserver.Server.Use` makes the *first* added middleware outermost, but its doc
  said the opposite, so `server.go` registered the guard second, i.e. inside session gating. The auth
  tests wrapped the handlers by hand in the intended order, so they tested an order production did
  not have.
- **Fix**: mount request guards only through `authhttp.Protect` (guard first, then session), and have
  tests serve `httpserver.Server.Handler()` — the real chain — instead of composing middleware
  themselves. `TestUseRunsMiddlewareInRegistrationOrder` pins the order.

### `latest=true` has no window: exited containers and old images stay in it

- **Symptoms**: after a deploy the CONTAINERS block showed `—` for running containers, an old image,
  and containers removed hours earlier (v0.2.4, 2026-09-23).
- **Root cause**: a container's series is keyed by its labels, `image` included, so every redeploy
  starts a new series; `latest=true` returns the newest point of *every* series inside the raw TTL.
  The UI merged them by name and let whichever series came last win.
- **Fix**: the UI decides what is current — per container and metric the newest series wins, and a
  container with no fresh sample is not shown (docs/SPEC.md §5). Never assume `latest` ages out.

### A Docker log stream is the only level the agent sees

- **Symptoms**: every dashboard request appeared as a `warn` event from the server container.
- **Root cause**: Docker log lines carry no priority; the agent maps stdout to `info` and stderr to
  `warn`, and Go's `log` package writes to stderr by default.
- **Fix**: the server's default `slog` logger sends Info to stdout and Warn/Error to stderr
  (`internal/platform/logging`). Any container you want to read well in EVENTS must do the same.
