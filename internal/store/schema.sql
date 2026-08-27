PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS identities (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  auth_type TEXT NOT NULL,
  encrypted_blob TEXT NOT NULL,
  encrypted_key_passphrase TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS folders (
  id TEXT PRIMARY KEY,
  parent_id TEXT,
  label TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT,
  FOREIGN KEY (parent_id) REFERENCES folders(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS hosts (
  id TEXT PRIMARY KEY,
  folder_id TEXT,
  label TEXT NOT NULL,
  hostname TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  protocol TEXT NOT NULL DEFAULT 'ssh',
  identity_id TEXT,
  jump_host_id TEXT,
  inline_identity_auth_type TEXT,
  inline_identity_encrypted_blob TEXT,
  inline_identity_encrypted_key_passphrase TEXT,
  last_connected_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT,
  FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL,
  FOREIGN KEY (identity_id) REFERENCES identities(id) ON DELETE SET NULL,
  FOREIGN KEY (jump_host_id) REFERENCES hosts(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS host_tags (
  host_id TEXT NOT NULL,
  tag_id TEXT NOT NULL,
  PRIMARY KEY (host_id, tag_id),
  FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
  FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS snippets (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  command TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  key_prefix TEXT NOT NULL UNIQUE,
  key_hash TEXT NOT NULL,
  scopes TEXT NOT NULL,
  expires_at TEXT,
  last_used_at TEXT,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS connection_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  host_id TEXT,
  host_label TEXT NOT NULL,
  hostname TEXT NOT NULL,
  port INTEGER NOT NULL,
  protocol TEXT NOT NULL DEFAULT 'ssh',
  jump_host_id TEXT,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  duration_seconds INTEGER
);

CREATE TABLE IF NOT EXISTS known_hosts (
  id TEXT PRIMARY KEY,
  hostname TEXT NOT NULL,
  port INTEGER NOT NULL,
  key_type TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  public_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(hostname, port)
);

CREATE TABLE IF NOT EXISTS change_log (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  entity TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  op TEXT NOT NULL,
  row_updated_at TEXT NOT NULL,
  origin_replica_id TEXT NOT NULL,
  payload TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_hosts_folder ON hosts(folder_id);
CREATE INDEX IF NOT EXISTS idx_folders_parent ON folders(parent_id);
CREATE INDEX IF NOT EXISTS idx_change_log_entity ON change_log(entity, entity_id);
