-- Hourly metric rollups and retention support (docs/SPEC.md §4.4–§4.5). Timestamps are unix
-- milliseconds in UTC; hour_ts is the start of the hour.
CREATE TABLE metric_rollups_hourly (
  host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  labels_json TEXT NOT NULL,        -- canonical (sorted-key) JSON object
  hour_ts     INTEGER NOT NULL,
  count       INTEGER NOT NULL,
  min         REAL NOT NULL,
  max         REAL NOT NULL,
  sum         REAL NOT NULL,        -- avg = sum / count; a zero-only hour has sum 0, count > 0
  PRIMARY KEY (host_id, name, labels_json, hour_ts)
) WITHOUT ROWID;
CREATE INDEX metric_rollups_hour_ts ON metric_rollups_hourly (hour_ts);

-- Single-row marker: every hour that starts before rolled_through has been aggregated.
CREATE TABLE rollup_state (
  id             INTEGER PRIMARY KEY CHECK (id = 1),
  rolled_through INTEGER NOT NULL
);

-- ts-leading indexes so bounded-batch purges do not scan whole tables.
CREATE INDEX metrics_ts ON metrics (ts);
CREATE INDEX checks_ts ON checks (ts);
CREATE INDEX ingestion_batches_received_at ON ingestion_batches (received_at);
