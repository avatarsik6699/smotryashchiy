-- Change 27: the latest whole JSON document a host reports per kind (docs/SPEC.md §4.1), such as
-- the tracked site's business summary. A newer batch replaces it; the host's deletion removes it.
CREATE TABLE documents (
  host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  received_at INTEGER NOT NULL,
  body        TEXT NOT NULL,
  PRIMARY KEY (host_id, kind)
) WITHOUT ROWID;
