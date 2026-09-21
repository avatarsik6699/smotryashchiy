-- Telemetry storage (docs/SPEC.md §4.2). All timestamps are unix milliseconds in UTC.
-- Record identity is the primary key, so INSERT OR IGNORE stores replayed/overlapping data once.
CREATE TABLE hosts (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER              -- NULL until the first stored batch (unknown stays unknown)
);

CREATE TABLE metrics (
  host_id        TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  labels_json    TEXT NOT NULL,     -- canonical (sorted-key) JSON object
  ts             INTEGER NOT NULL,
  value          REAL NOT NULL,     -- zero is a real value
  schema_version TEXT NOT NULL,
  PRIMARY KEY (host_id, name, labels_json, ts)
) WITHOUT ROWID;
CREATE INDEX metrics_series_ts ON metrics (host_id, name, ts);

CREATE TABLE checks (
  host_id        TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  ts             INTEGER NOT NULL,
  status         TEXT NOT NULL CHECK (status IN ('ok', 'warn', 'critical')),
  meta_json      TEXT NOT NULL,
  schema_version TEXT NOT NULL,
  PRIMARY KEY (host_id, name, ts)
) WITHOUT ROWID;

CREATE TABLE events (
  host_id        TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  ts             INTEGER NOT NULL,
  level          TEXT NOT NULL CHECK (level IN ('info', 'warn', 'error', 'critical')),
  message        TEXT NOT NULL,
  labels_json    TEXT NOT NULL,
  schema_version TEXT NOT NULL,
  PRIMARY KEY (host_id, ts, level, message, labels_json)
) WITHOUT ROWID;
CREATE INDEX events_ts ON events (ts DESC);

CREATE TABLE ingestion_batches (
  host_id         TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  idempotency_key TEXT NOT NULL,
  received_at     INTEGER NOT NULL,
  record_count    INTEGER NOT NULL,
  PRIMARY KEY (host_id, idempotency_key)
) WITHOUT ROWID;
