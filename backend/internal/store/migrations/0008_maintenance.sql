-- Periods during which no incident is opened for the monitors in scope.
-- Scope is one monitor (monitor_id), one namespace (namespace), or, with
-- neither set, every monitor.
CREATE TABLE maintenance_windows (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    starts_at  INTEGER NOT NULL,
    ends_at    INTEGER NOT NULL,
    namespace  TEXT    NOT NULL DEFAULT '',
    monitor_id INTEGER REFERENCES monitors (id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (namespace = '' OR monitor_id IS NULL)
);

CREATE INDEX idx_maintenance_ends ON maintenance_windows (ends_at);
