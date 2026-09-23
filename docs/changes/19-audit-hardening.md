# CHANGE 19 — Hardening from the production audit

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `19` |
| Slug | `audit-hardening` |
| Title | Hardening from the production audit |
| Status | `active` |
| Branch | `feature/19-audit-hardening` |

---

## Goal

The 2026-09-23 audit of the monitoring server and infraege.ru confirmed eight defects. Three are in
the server: a same-site page can write through the session cookie, the collect rate limiter can be
bypassed and grown without bound, and there is no HSTS. Two are in the agent: ticks drift to ~18 s,
and container logs are stored twice while kernel lines have no source label. Three are in the
analytics numbers: "today" is a rolling 24 h, the query string is stored in paths, and referrers are
counted per pageview. This change fixes them per `docs/SPEC.md` v1.19 (§4d, §4g, §4h, §4i, §5). The
audit's ops findings (backup cron, uptime target, GHCR image) were fixed on the VPS directly and are
not part of this change.

---

## Backlog

<!-- This list is OPEN, not a fixed scope: /work appends new items here when the architect reports
     findings/fixes/follow-ups mid-session — it does not fix them off-list.
     Group items by area (Backend / Frontend / Infra / Data, etc.).
     ID scheme: B=Backend · F=Frontend · I=Infra · D=Data · T=other (ungrouped)
     Each item: `ID` description — _Depends on:_ ID, ID or —
     IDs are stable after assignment — never renumber. Mark removed items as ~~BN~~ (removed).
     New items always take the next unused ID in their group, appended at the end. -->

### Backend
- [x] `B1` Cross-origin write guard (§4d): middleware on the API chain refuses unsafe-method `/api/*`
      requests (except `POST /api/collect` and `POST /api/enroll`) with `403` when `Sec-Fetch-Site`
      is present and not `same-origin`/`none`, or, without it, when `Origin` is present and is not the
      request's own origin. A request with a body must be `application/json` (`415`). Tests: the audit's
      reproduction (`text/plain` POST with a foreign same-site Origin → refused, nothing created),
      a same-origin JSON POST passes, a headerless client passes, login and logout are covered, and
      collect/enroll are exempt. — _Depends on:_ —
- [x] `B2` HSTS (§4g): every response on the TLS listener carries
      `Strict-Transport-Security: max-age=31536000`, and the ACME/redirect listener never sends it.
      Test on `ServeHTTPS` and the redirect handler. — _Depends on:_ —
- [x] `B3` Rate limiter (§4i): key an IPv6 address by its `/64` (IPv4 unchanged), and bound
      `ratelimit.Limiter` at 10 000 keys by evicting idle keys and then the least recently seen ones.
      Pruning must not become O(n) on every new key under a flood. Tests: two addresses in one `/64`
      share a bucket, and the map never exceeds the bound when all keys are active. — _Depends on:_ —
- [x] `B4` `/api/collect`: check the rate limit before `setCORS` (no DB read for a limited
      request), and let `setCORS` accept `www.` + domain like `originMatchesSite`. — _Depends on:_ B3
- [x] `B5` Analytics storage (§4i): keep only the pathname of `url` (drop `?query` and `#fragment`
      server-side, also when an old snippet sends them), and drop a referrer whose hostname is the
      site's domain or `www.` + domain. Tests at service level. — _Depends on:_ —
- [x] `B6` Stats ranges (§4i): `today` from 00:00 UTC of the current day, `7d`/`30d` from 00:00 UTC
      6/29 days earlier. Tests at the day boundary (23:59 vs 00:01 UTC). — _Depends on:_ —
- [x] `B7` `track.js` (§4i): send `pathname` only and dedupe on it, and send `referrer` only with the
      first pageview of a page load. Extend the node-vm harness in `track_test.go`: a query-only
      change sends nothing, and the second pageview has an empty referrer. — _Depends on:_ —
- [x] `B8` Agent Docker metrics (§4h): fetch container stats concurrently (at most 8 in flight)
      under a per-tick deadline of `min(interval − 1 s, 5 s)`; a container whose stats miss the
      deadline is omitted for that tick. Test with a fake socket whose stats take ~2 s each: 9
      containers finish within the deadline. — _Depends on:_ —
- [x] `B9` Agent journald (§4h): skip entries carrying `CONTAINER_NAME`, and label an entry without
      `_SYSTEMD_UNIT` as `unit=<SYSLOG_IDENTIFIER>`. Tests with journal JSON lines captured in the
      audit's shape (container line, kernel `[UFW BLOCK]` line, normal unit line). — _Depends on:_ —

### Frontend
- [x] `F1` Analytics site detail: a muted `days in UTC` note next to the range toggle (§5). Unit test
      plus axe check; Playwriter screenshot and console check. — _Depends on:_ B6

### Other
- [x] `T1` `docs/KNOWN_GOTCHAS.md`: (a) SameSite=Lax does not stop same-site subdomains, so a
      `text/plain` POST needs no preflight; (b) Docker's `journald` log driver duplicates container
      lines into the journal; (c) `stats?stream=false` blocks ~2 s per container. — _Depends on:_ B1, B8, B9

<!-- Test execution is governed by `docs/STACK.md`'s Fast Gate (per task) and Full Gate (per ship).
     Do not duplicate that list here. -->

---

## Files

### Create / modify
~~~
internal/auth/interfaces/http/middleware.go          (or a new platform middleware, B1)
internal/auth/interfaces/http/*_test.go
cmd/smotryashchiy/server.go                          (wiring B1, B2)
internal/platform/httpserver/server.go, server_test.go   (B2)
internal/platform/ratelimit/ratelimit.go, ratelimit_test.go   (B3)
internal/analytics/interfaces/http/collect.go, collect_test.go   (B4)
internal/analytics/application/service.go, service_test.go       (B5, B6)
internal/analytics/interfaces/http/track.js, track_test.go       (B7)
internal/agent/collect/docker.go, collect_test.go (or docker_test.go)   (B8)
internal/agent/collect/journald.go and its tests                 (B9)
web/src/components/Analytics/SiteDetails.tsx, *.module.css, tests (F1)
docs/KNOWN_GOTCHAS.md                                (T1)
docs/SPEC.md                                         (§4d, §4g, §4h, §4i, §5 — done by /plan)
~~~

### Do NOT touch
- Existing pageview rows (including the 72 test rows and `/?_=…` paths): no cleanup migration.
- Storage schema and migrations; `pageview_rollups_daily` (unused, out of scope).
- Bot User-Agent list and UA parsing; alerting; `deploy/` compose files.
- infraegev2 (its sshd hardening is tracked there, not here).

---

## Contracts

See `docs/SPEC.md` §4d, §4g, §4h, §4i, §5 and the Files list above. Do not hand-copy the
schema, endpoints, types, or env vars into this file — the codebase and `SPEC.md` are the source
of truth; this file only tracks what to build and what's left.

---

## Gate Checks

> Fast Gate runs per task in `/work`; Full Gate and (with `--release`) Release Gate run once in
> `/ship`. Both are defined in [docs/STACK.md](../STACK.md) — this section only records
> change-specific overrides.

Change-specific checks, run against a local server before ship:

```bash
# B1: the audit's reproduction must now be refused and create nothing
curl -s -b jar -o /dev/null -w '%{http_code}\n' -H 'Origin: https://evil.infraege.ru' \
  -H 'Content-Type: text/plain' -d '{"name":"x","domain":"x.example"}' http://127.0.0.1:18777/api/sites
# expected: 403 (and GET /api/sites unchanged)
```

After release, verify on production: the dashboard still logs in and adds/removes a target; the
agent on infraege.ru delivers every ~10 s (raw `uptime.seconds` spacing); no `docker.service` copies
of container lines; kernel lines carry `unit=kernel`; `sre.infraege.ru` sends HSTS.

---

## Architect Review Notes

Use this section after manual product, UX, API, or workflow verification. This is the human-facing
channel for post-implementation fixes.

Add one unchecked checkbox per issue the agent must fix before the change can ship. Keep each item
independently fixable and describe observed behavior plus expected behavior. If the fix may change
SPEC/API/schema/security behavior, say so explicitly in the note.

The agent resolves these items through `/work [XX] review`. Leave an item unchecked while it is
still open. Check it off only after the fix is implemented and re-verified. If manual verification
found nothing, keep the default checked line below.

- [x] No architect review issues recorded

---

## Implementation Notes

- B3 bounds and re-keys `ratelimit.Limiter` only. The login limiter (`authhttp/login_limiter.go`) is a
  separate map keyed by the full address: its entries appear only on failed logins and expire after
  15 min, and a ≥ 24-byte password makes per-/64 bypass irrelevant to brute force. Residual risk: many
  failing sources can still grow that map until entries expire. Left out of scope.
- F1: the `impeccable` skill was not consulted — no design decision was made; the note reuses the
  existing muted-text token next to the toggle exactly as SPEC §5 specifies.

---

## Commit Message

```
fix(change-19): audit hardening — CSRF guard, agent drift, clean stats
```
