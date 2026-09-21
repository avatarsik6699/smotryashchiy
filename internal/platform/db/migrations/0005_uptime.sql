-- Uptime prober (docs/SPEC.md §4e). Timestamps are unix milliseconds in UTC.
CREATE TABLE uptime_targets (
  id               TEXT PRIMARY KEY,
  name             TEXT NOT NULL UNIQUE,
  kind             TEXT NOT NULL CHECK (kind IN ('http', 'tcp', 'tls')),
  address          TEXT NOT NULL,        -- http(s) URL for http, host:port for tcp and tls
  interval_seconds INTEGER NOT NULL CHECK (interval_seconds BETWEEN 30 AND 3600),
  created_at       INTEGER NOT NULL
);

CREATE TABLE uptime_results (
  target_id       TEXT NOT NULL REFERENCES uptime_targets(id) ON DELETE CASCADE,
  ts              INTEGER NOT NULL,
  ok              INTEGER NOT NULL CHECK (ok IN (0, 1)),
  latency_ms      INTEGER,               -- NULL for a failed check: no measurement is not 0 ms
  status_code     INTEGER,
  error           TEXT,
  cert_expires_at INTEGER,
  PRIMARY KEY (target_id, ts)
) WITHOUT ROWID;
CREATE INDEX uptime_results_ts ON uptime_results (ts);
