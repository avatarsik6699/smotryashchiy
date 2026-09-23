-- Change 20 (docs/SPEC.md §4i): the daily pageview rollups were written hourly and never read —
-- every stats range fits inside the raw TTL. Every row was derivable from raw pageviews.
DROP TABLE IF EXISTS pageview_rollups_daily;
