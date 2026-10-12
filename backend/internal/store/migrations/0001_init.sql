CREATE TABLE monitors (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL UNIQUE,
    type             TEXT    NOT NULL CHECK (type IN ('http', 'tcp')),
    target           TEXT    NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds > 0),
    timeout_seconds  INTEGER NOT NULL CHECK (timeout_seconds > 0),
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL
);

CREATE TABLE check_results (
    id          INTEGER PRIMARY KEY,
    monitor_id  INTEGER NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    checked_at  INTEGER NOT NULL,
    up          INTEGER NOT NULL,
    latency_ms  INTEGER NOT NULL,
    status_code INTEGER,
    error       TEXT
);

CREATE INDEX idx_check_results_monitor_time ON check_results (monitor_id, checked_at);
CREATE INDEX idx_check_results_time ON check_results (checked_at);
