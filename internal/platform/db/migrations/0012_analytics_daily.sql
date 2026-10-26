-- Change 36 (docs/SPEC.md §4i): daily analytics aggregates, kept 25 months so a season compares
-- with the last one after the raw rows are gone. `day` is the site's local date (YYYY-MM-DD);
-- `dimension` '' with `value` '' is the day's total. Every count is additive across days: the
-- visitor hash and every visit belong to exactly one site day.
CREATE TABLE analytics_daily (
  site_id     TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  day         TEXT NOT NULL,
  dimension   TEXT NOT NULL,
  value       TEXT NOT NULL,
  visitors    INTEGER NOT NULL,
  visits      INTEGER NOT NULL,
  pageviews   INTEGER NOT NULL,
  bounces     INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  PRIMARY KEY (site_id, dimension, day, value)
) WITHOUT ROWID;
CREATE INDEX analytics_daily_day ON analytics_daily (day);
