ALTER TABLE monitors ADD COLUMN failure_threshold INTEGER NOT NULL DEFAULT 3 CHECK (failure_threshold > 0);

CREATE TABLE incidents (
    id          INTEGER PRIMARY KEY,
    monitor_id  INTEGER NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    started_at  INTEGER NOT NULL,
    resolved_at INTEGER,
    cause       TEXT    NOT NULL
);

CREATE INDEX idx_incidents_started ON incidents (started_at);
-- A monitor has at most one open incident.
CREATE UNIQUE INDEX idx_incidents_open ON incidents (monitor_id) WHERE resolved_at IS NULL;

CREATE TABLE notification_channels (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL CHECK (type IN ('webhook')),
    url        TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
