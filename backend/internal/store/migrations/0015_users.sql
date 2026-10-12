-- Accounts, their sessions, how LDAP groups map to roles, and a record of
-- who changed what.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    display_name  TEXT    NOT NULL DEFAULT '',
    email         TEXT    NOT NULL DEFAULT '',
    -- 'local' accounts have a password here; 'ldap' accounts are checked
    -- against the directory and their role comes from its groups.
    source        TEXT    NOT NULL DEFAULT 'local',
    password_hash TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL,
    -- The namespaces (and groups) an editor or viewer may see, as a JSON
    -- array; the single entry "*" means all of them.
    namespaces    TEXT    NOT NULL DEFAULT '[]',
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER
);

-- A signed-in browser. Only the hash of the token is kept: the token itself
-- is in the browser's cookie.
CREATE TABLE sessions (
    token_hash   TEXT    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX idx_sessions_user ON sessions (user_id);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

CREATE TABLE ldap_group_roles (
    id         INTEGER PRIMARY KEY,
    group_dn   TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    role       TEXT    NOT NULL,
    namespaces TEXT    NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL
);

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    at          INTEGER NOT NULL,
    user_id     INTEGER,
    username    TEXT    NOT NULL,
    action      TEXT    NOT NULL,
    object_type TEXT    NOT NULL DEFAULT '',
    object_id   INTEGER NOT NULL DEFAULT 0,
    summary     TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_audit_at ON audit_log (at);
