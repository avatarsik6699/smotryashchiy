-- Change 35 (docs/SPEC.md §4i): visits, sources, geography and devices. Times are unix
-- milliseconds in UTC, like the rest of the schema.

-- The site's day (reports and the visitor-hash salt turn over at its local midnight).
ALTER TABLE sites ADD COLUMN timezone TEXT NOT NULL DEFAULT 'Europe/Moscow';

-- One visit: the same daily visitor hash with less than 30 minutes between pageviews. What is
-- known at its first pageview (source, UTM, place, device) belongs to the visit.
CREATE TABLE visits (
  id              TEXT PRIMARY KEY,
  site_id         TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  visitor_hash    TEXT NOT NULL,
  started_at      INTEGER NOT NULL,
  last_at         INTEGER NOT NULL,
  entry_path      TEXT NOT NULL,
  exit_path       TEXT NOT NULL,
  pageviews       INTEGER NOT NULL,
  hostname        TEXT NOT NULL DEFAULT '',
  referrer_domain TEXT NOT NULL DEFAULT '',
  source          TEXT NOT NULL DEFAULT '',
  channel         TEXT NOT NULL,
  utm_source      TEXT NOT NULL DEFAULT '',
  utm_medium      TEXT NOT NULL DEFAULT '',
  utm_campaign    TEXT NOT NULL DEFAULT '',
  utm_content     TEXT NOT NULL DEFAULT '',
  utm_term        TEXT NOT NULL DEFAULT '',
  paid            INTEGER NOT NULL DEFAULT 0,
  country         TEXT NOT NULL DEFAULT '',
  region          TEXT NOT NULL DEFAULT '',
  browser         TEXT NOT NULL DEFAULT '',
  browser_version TEXT NOT NULL DEFAULT '',
  os              TEXT NOT NULL DEFAULT '',
  os_version      TEXT NOT NULL DEFAULT '',
  device          TEXT NOT NULL DEFAULT '',
  screen          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX visits_lookup ON visits (site_id, visitor_hash, last_at);
CREATE INDEX visits_site_started ON visits (site_id, started_at);
CREATE INDEX visits_last ON visits (last_at);

-- Pageviews before this change have no visit; they still count as pageviews and visitors.
ALTER TABLE pageviews ADD COLUMN visit_id TEXT REFERENCES visits(id) ON DELETE CASCADE;
ALTER TABLE pageviews ADD COLUMN hostname TEXT NOT NULL DEFAULT '';
CREATE INDEX pageviews_visit ON pageviews (visit_id);
-- The purge deletes by age; until now it scanned the table.
CREATE INDEX pageviews_ts ON pageviews (ts);
