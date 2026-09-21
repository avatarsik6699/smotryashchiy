-- Host enrollment and tunnel peers (docs/SPEC.md §4b). Timestamps are unix milliseconds in UTC.
CREATE TABLE host_enrollments (
  host_id     TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  secret_hash TEXT NOT NULL UNIQUE,   -- hex SHA-256 of the one-time secret; the secret is never stored
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER,                -- NULL until consumed; consumed at most once
  PRIMARY KEY (host_id, secret_hash)
) WITHOUT ROWID;

CREATE TABLE host_peers (
  host_id     TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
  public_key  TEXT NOT NULL UNIQUE,   -- base64 WireGuard public key
  tunnel_ip   TEXT NOT NULL UNIQUE,   -- the peer's /32 inside the tunnel subnet
  enrolled_at INTEGER NOT NULL
) WITHOUT ROWID;
