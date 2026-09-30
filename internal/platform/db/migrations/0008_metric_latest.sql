-- Change 24: bound latest=true reads independently of the raw telemetry volume.
-- Migrate executes this file inside one transaction, so backfill and triggers
-- become visible together or not at all.
CREATE TABLE metric_latest (
  host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  labels_json TEXT NOT NULL,
  ts          INTEGER NOT NULL,
  value       REAL NOT NULL,
  PRIMARY KEY (host_id, name, labels_json)
) WITHOUT ROWID;

INSERT INTO metric_latest (host_id, name, labels_json, ts, value)
SELECT m.host_id, m.name, m.labels_json, m.ts, m.value
FROM metrics AS m
JOIN (
  SELECT host_id, name, labels_json, MAX(ts) AS ts
  FROM metrics
  GROUP BY host_id, name, labels_json
) AS newest
  ON m.host_id = newest.host_id
 AND m.name = newest.name
 AND m.labels_json = newest.labels_json
 AND m.ts = newest.ts;

CREATE TRIGGER metrics_latest_insert AFTER INSERT ON metrics
BEGIN
  INSERT INTO metric_latest (host_id, name, labels_json, ts, value)
  VALUES (NEW.host_id, NEW.name, NEW.labels_json, NEW.ts, NEW.value)
  ON CONFLICT (host_id, name, labels_json) DO UPDATE SET
    ts = excluded.ts,
    value = excluded.value
  WHERE excluded.ts > metric_latest.ts;
END;

CREATE TRIGGER metrics_latest_delete AFTER DELETE ON metrics
WHEN EXISTS (
  SELECT 1 FROM metric_latest
  WHERE host_id = OLD.host_id AND name = OLD.name
    AND labels_json = OLD.labels_json AND ts = OLD.ts
)
BEGIN
  DELETE FROM metric_latest
  WHERE host_id = OLD.host_id AND name = OLD.name AND labels_json = OLD.labels_json;
  INSERT INTO metric_latest (host_id, name, labels_json, ts, value)
  SELECT host_id, name, labels_json, ts, value
  FROM metrics
  WHERE host_id = OLD.host_id AND name = OLD.name AND labels_json = OLD.labels_json
  ORDER BY ts DESC LIMIT 1;
END;
