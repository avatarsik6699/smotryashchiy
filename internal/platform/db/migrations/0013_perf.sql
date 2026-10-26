-- Change 38 (docs/SPEC.md §4.2): reads the dashboard repeats every minute stay bounded as the raw
-- telemetry grows.

-- Error and critical events of the last hour: a level-first index, so a rare level is a short
-- range instead of a walk through the whole time index.
CREATE INDEX events_level_ts ON events (level, ts);

-- The newest check of every (host, name), kept by triggers like metric_latest (0008): the checks
-- list no longer groups the whole table on every refresh.
CREATE TABLE check_latest (
  host_id   TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name      TEXT NOT NULL,
  ts        INTEGER NOT NULL,
  status    TEXT NOT NULL,
  meta_json TEXT NOT NULL,
  PRIMARY KEY (host_id, name)
) WITHOUT ROWID;

INSERT INTO check_latest (host_id, name, ts, status, meta_json)
SELECT c.host_id, c.name, c.ts, c.status, c.meta_json
FROM checks AS c
JOIN (SELECT host_id, name, MAX(ts) AS ts FROM checks GROUP BY host_id, name) AS newest
  ON c.host_id = newest.host_id AND c.name = newest.name AND c.ts = newest.ts;

CREATE TRIGGER checks_latest_insert AFTER INSERT ON checks
BEGIN
  INSERT INTO check_latest (host_id, name, ts, status, meta_json)
  VALUES (NEW.host_id, NEW.name, NEW.ts, NEW.status, NEW.meta_json)
  ON CONFLICT (host_id, name) DO UPDATE SET
    ts = excluded.ts,
    status = excluded.status,
    meta_json = excluded.meta_json
  WHERE excluded.ts > check_latest.ts;
END;

CREATE TRIGGER checks_latest_delete AFTER DELETE ON checks
WHEN EXISTS (SELECT 1 FROM check_latest WHERE host_id = OLD.host_id AND name = OLD.name AND ts = OLD.ts)
BEGIN
  DELETE FROM check_latest WHERE host_id = OLD.host_id AND name = OLD.name;
  INSERT INTO check_latest (host_id, name, ts, status, meta_json)
  SELECT host_id, name, ts, status, meta_json FROM checks
  WHERE host_id = OLD.host_id AND name = OLD.name
  ORDER BY ts DESC LIMIT 1;
END;
