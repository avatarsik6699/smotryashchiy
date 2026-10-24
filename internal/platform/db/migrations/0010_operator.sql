-- Change 30: the operator console for the tracked site (docs/SPEC.md §4j). Times are unix
-- milliseconds in UTC, like the rest of the schema.

-- Commands the operator queues for one host; the host's agent claims and answers them.
CREATE TABLE operator_commands (
  id          TEXT PRIMARY KEY,
  host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  args_json   TEXT NOT NULL,
  reason      TEXT,
  status      TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed', 'expired')),
  created_at  INTEGER NOT NULL,
  started_at  INTEGER,
  finished_at INTEGER,
  result_json TEXT,
  error       TEXT
);
CREATE INDEX operator_commands_host_status ON operator_commands (host_id, status, created_at);
CREATE INDEX operator_commands_created ON operator_commands (created_at);

-- Who did what through the console. Append-only: rows cannot be changed, and cannot be deleted
-- before they are three years old (the retention job deletes older ones).
CREATE TABLE audit_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  at          INTEGER NOT NULL,
  action      TEXT NOT NULL,
  target      TEXT NOT NULL DEFAULT '',
  detail_json TEXT NOT NULL DEFAULT '{}',
  client      TEXT NOT NULL DEFAULT '',
  outcome     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log (at);

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN
  SELECT RAISE(ABORT, 'audit_log is append-only');
END;

CREATE TRIGGER audit_log_no_early_delete BEFORE DELETE ON audit_log
WHEN old.at > (CAST(strftime('%s', 'now') AS INTEGER) - 3 * 365 * 86400) * 1000
BEGIN
  SELECT RAISE(ABORT, 'audit_log rows are kept for three years');
END;

-- Incidents the operator records by hand (a personal-data leak, an outage, anything else).
CREATE TABLE incidents (
  id                TEXT PRIMARY KEY,
  title             TEXT NOT NULL,
  kind              TEXT NOT NULL CHECK (kind IN ('leak', 'outage', 'other')),
  detected_at       INTEGER NOT NULL,
  scope             TEXT NOT NULL DEFAULT '',
  people_affected   INTEGER,
  measures          TEXT NOT NULL DEFAULT '',
  rkn_primary_at    INTEGER,
  rkn_final_at      INTEGER,
  users_notified_at INTEGER,
  notes             TEXT NOT NULL DEFAULT '',
  closed_at         INTEGER,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL
);
CREATE INDEX incidents_detected ON incidents (detected_at);
