-- Key/value settings owned by the platform; currently holds the admin password bcrypt hash.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
