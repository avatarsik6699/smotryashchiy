-- Site visitor analytics (docs/SPEC.md §4i): a bounded context of its own, unrelated to host
-- telemetry. Timestamps are unix milliseconds in UTC. No raw IP or persistent visitor identifier
-- is ever stored here — visitor_hash is a daily-rotating salted hash (see internal/analytics).
CREATE TABLE sites (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  domain     TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

CREATE TABLE pageviews (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id         TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  ts              INTEGER NOT NULL,
  path            TEXT NOT NULL,
  referrer_domain TEXT NOT NULL DEFAULT '',
  visitor_hash    TEXT NOT NULL,
  browser         TEXT NOT NULL DEFAULT '',
  os              TEXT NOT NULL DEFAULT '',
  device          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX pageviews_site_ts ON pageviews (site_id, ts);

CREATE TABLE pageview_rollups_daily (
  site_id        TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  day            INTEGER NOT NULL,  -- UTC day, unix ms at 00:00:00
  path           TEXT NOT NULL,
  unique_visitors INTEGER NOT NULL,
  pageviews      INTEGER NOT NULL,
  PRIMARY KEY (site_id, day, path)
) WITHOUT ROWID;
