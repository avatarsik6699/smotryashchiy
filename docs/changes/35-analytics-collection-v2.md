# CHANGE 35 — Analytics collection v2: visits, sources, geography, devices

<!-- TOKEN BUDGET: keep this file under 10,000 tokens. Be concise. -->

## Change Metadata

| Field | Value |
|-------|-------|
| Change | `35` |
| Slug | `analytics-collection-v2` |
| Title | Analytics collection v2: visits, sources, geography, devices |
| Status | `active` |
| Branch | `feature/35-analytics-collection-v2` |

---

## Goal

First of three changes that bring site analytics to the Plausible / Umami level: stages S1–S2 of
the 2026-10-14 «Аналитика и SEO» plan. Change 36 adds queries and history, Change 37 the dashboard.

What it collects:
- visits;
- sources and channels with the five UTM parameters;
- country and region;
- screen size, browser and OS versions;
- the page hostname.

The server derives everything it can from the request. The tracker v2 adds only what the server
cannot see (UTM, paid-click flag, window width, hostname) and sends the referrer as an origin. The
old `{site,url,referrer}` beacon keeps working, so no tracked site has to redeploy.

Architect decisions of 2026-10-26:
- scope S1–S4;
- geography: country and region from DB-IP Lite, the IP is never stored;
- raw analytics kept 90 days (aggregates for 25 months come in Change 36);
- click filters and period comparison in Change 37, without state in the URL.

---

## Backlog

### Backend
- [x] `B1` Sites get a `timezone` (IANA; migration default `Europe/Moscow`, optional on create,
  validated). The visitor-hash salt turns over at the site's local midnight, not UTC (SPEC §4i).
  `time/tzdata` is embedded so the image needs no zoneinfo. — _Depends on:_ —
- [x] `B2` Beacon v2, accepted alongside the old shape, every field optional and capped:
  - `utm` — the five keys only, lowercased, ≤ 100 bytes each, `[a-z0-9._-]`, otherwise `(invalid)`;
  - `paid` — a boolean;
  - `w` — window width;
  - `h` — hostname, kept only when it is the site's domain or `www.` plus it.

  The public answer stays `204`. — _Depends on:_ —
- [x] `B3` Visits:
  - a visit is the same daily visitor hash with less than 30 minutes since its last pageview;
  - it carries start/last time, entry and exit path, pageview count and everything known at its
    first pageview — hostname, referrer, source, channel, UTM, paid flag, country, region,
    browser + major version, OS + version, device, screen group;
  - pageviews get `visit_id` and `hostname`;
  - lookup by `(site_id, visitor_hash, last_at)`, in one transaction with the pageview insert. — _Depends on:_ B1, B2
- [x] `B4` Sources and channels: a referrer host, UTM and paid flag map to a normalized source name
  and one channel, first matching rule wins:
  - paid search, organic search, paid social, social, messengers, AI assistants, email, referral,
    direct;
  - the dictionary covers the Russian-speaking audience (Yandex, Google, Mail.ru, VK, OK, Dzen,
    Telegram, YouTube, ChatGPT, Perplexity, Gemini, Алиса…) and is unit-tested. — _Depends on:_ —
- [x] `B5` Geography: optional `SMOTRYASHCHIY_GEOIP_PATH` (MMDB, DB-IP City Lite).
  - Country = ISO code; region = the English subdivision name (DB-IP Lite has no ISO 3166-2 codes).
  - No file or no record means an empty value and everything else works.
  - The IP is used only at intake.
  - Reader `github.com/oschwald/maxminddb-golang/v2` (pure Go, no cgo). — _Depends on:_ —
- [x] `B6` User-Agent: browser major version and OS version (Windows/macOS/iOS/Android, where the
  UA still carries one); screen group from `w`: mobile < 576, tablet < 992, laptop < 1440, desktop. — _Depends on:_ —
- [x] `B7` Storage hygiene:
  - an index on `pageviews(ts)` and `visits(last_at)` for the purge;
  - visits purged with the pageviews;
  - `SMOTRYASHCHIY_ANALYTICS_RETENTION_DAYS` defaults to 90. — _Depends on:_ B3
- [x] `B8` The existing `GET /api/sites/{id}/stats` keeps its shape and also returns visits,
  bounce rate and average visit duration. Change 37 replaces its consumer. — _Depends on:_ B3

### Frontend
- [x] `F1` Tracker v2 (`track.js`), backward compatible:
  - `v:2`;
  - UTM and paid flag (`yclid`/`gclid` present, value never sent) from the first pageview of a load
    only;
  - `w`, `h`;
  - referrer as an origin, closing audit A5;
  - `data-exclude` checked on every navigation, as already;
  - no query string ever sent. — _Depends on:_ B2

### Infra
- [x] `I1` GeoIP file on the monitoring host:
  - a monthly update recipe (`deploy/geoip/` script + systemd timer, or a documented cron) that
    downloads DB-IP City Lite, checks it opens and swaps it atomically;
  - the server reopens a changed file without a restart (mtime check on lookup);
  - DEPLOY/RUNBOOK and the `.env.example`;
  - CC BY attribution noted for Change 37's UI. — _Depends on:_ B5

### Other
- [x] `T1` Tests:
  - a synthetic day of beacons gives hand-counted visits, bounces, entry/exit, channels;
  - old-format beacons still store;
  - salt turnover at site midnight;
  - GeoIP lookup against a small test MMDB, or a fake reader when the format can't be built in tests;
  - purge. — _Depends on:_ B1–B8, F1
- [x] `T2` SPEC §4i/§1.3, `.env.example`, KNOWN_GOTCHAS. Follow-up for kodumka.ru: its privacy text
  names the new derived fields (country/region, UTM) — an infraegev2 Backlog item, not this repo. — _Depends on:_ B1–B7

---

## Files

### Create / modify
~~~
internal/analytics/{domain,application,infrastructure,interfaces/http}/*
internal/platform/db/migrations/0011_analytics_v2.sql
internal/platform/config/config.go (+ test), cmd/smotryashchiy wiring
internal/analytics/interfaces/http/track.js (+ track_test.go)
deploy/geoip/* (new), docs/SPEC.md, docs/DEPLOY.md, docs/RUNBOOK.md, docs/KNOWN_GOTCHAS.md, .env.example
~~~

### Do NOT touch
- The dashboard UI (Change 37) and the query API (Change 36).
- Host telemetry, uptime, operator console.
- The tracked site's snippet: the old tag keeps working.

---

## Contracts

See `docs/SPEC.md` §4i and the Files list above.

---

## Gate Checks

Fast Gate per task; Full Gate at ship. Change-specific: `go test ./internal/analytics/...` with the
synthetic-day fixture.

---

## Architect Review Notes

- [x] No architect review issues recorded

---

## Implementation Notes

- DB-IP City Lite (checked on the 2026-10 file) carries the country ISO code and Russian name, but
  for regions only an English name, no ISO 3166-2 code. The region is stored as that name.
- GeoIP lookups hold the read lock for the whole decode: `maxminddb` `Close` unmaps the file at once,
  so a reload while a lookup reads the old map could crash.
- `TestGeoIPReadsDBIPCityLite` needs the real 120 MB file (`SMOTRYASHCHIY_TEST_GEOIP`). It passed
  locally against the 2026-10 file and is skipped in CI.
- Gate: Full Gate PASS (13 rows) on the change branch, 2026-10-26.
- LSP: `gopls check` reports the module outside the editor workspace. `go vet` and the tests stand
  in for it, as STACK allows.
- Follow-up outside this repo: kodumka.ru's `/privacy` names the new derived fields (infraegev2
  Change 229 `P1`, shipped with that release).

---

## Commit Message

```
feat(change-35): analytics collection v2 — visits, sources, geography, devices
```
