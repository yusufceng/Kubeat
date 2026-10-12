-- migrate: foreign_keys=off
--
-- More kinds of monitors. The monitors table is rebuilt because SQLite cannot
-- drop the CHECK constraints that limited it to HTTP and TCP; with foreign
-- keys on, dropping the old table would delete every result along with it.
--
-- The options of a check move from one column each into a JSON object keyed
-- by monitor type, e.g. {"http": {"method": "GET", ...}}: every type has its
-- own options and a column per option does not scale. The type itself is
-- validated by the server.
CREATE TABLE monitors_new (
    id                     INTEGER PRIMARY KEY,
    name                   TEXT    NOT NULL UNIQUE,
    type                   TEXT    NOT NULL,
    target                 TEXT    NOT NULL,
    interval_seconds       INTEGER NOT NULL CHECK (interval_seconds > 0),
    timeout_seconds        INTEGER NOT NULL CHECK (timeout_seconds > 0),
    failure_threshold      INTEGER NOT NULL DEFAULT 3 CHECK (failure_threshold > 0),
    enabled                INTEGER NOT NULL DEFAULT 1,
    created_at             INTEGER NOT NULL,
    namespace              TEXT    NOT NULL DEFAULT '',
    source                 TEXT    NOT NULL DEFAULT '',
    description            TEXT    NOT NULL DEFAULT '',
    -- How soon a failed check is repeated while the outage is not confirmed
    -- yet; 0 means the normal interval.
    retry_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (retry_interval_seconds >= 0),
    -- An inverted monitor is up while its target fails.
    inverted               INTEGER NOT NULL DEFAULT 0,
    -- The secret in the URL a push monitor's source reports to.
    push_token             TEXT    NOT NULL DEFAULT '',
    settings               TEXT    NOT NULL DEFAULT '{}'
);

INSERT INTO monitors_new (id, name, type, target, interval_seconds, timeout_seconds, failure_threshold, enabled, created_at,
                          namespace, source, settings)
SELECT id, name, type, target, interval_seconds, timeout_seconds, failure_threshold, enabled, created_at,
       namespace, source,
       CASE type
           WHEN 'http' THEN json_object('http', json_object(
               'method', http_method,
               'expected_status', http_expected_status,
               'keyword', http_keyword,
               'headers', json(http_headers),
               'skip_tls_verify', json(CASE tls_skip_verify WHEN 0 THEN 'false' ELSE 'true' END),
               -- Redirects were always followed, up to the HTTP client's limit.
               'max_redirects', 10))
           ELSE '{}'
       END
FROM monitors;

DROP TABLE monitors;
ALTER TABLE monitors_new RENAME TO monitors;

CREATE UNIQUE INDEX idx_monitors_source ON monitors (namespace, source) WHERE source != '';
CREATE UNIQUE INDEX idx_monitors_push_token ON monitors (push_token) WHERE push_token != '';

-- The latest report of each push monitor; the scheduler turns it into a
-- check result once per interval.
CREATE TABLE push_reports (
    monitor_id  INTEGER PRIMARY KEY REFERENCES monitors (id) ON DELETE CASCADE,
    received_at INTEGER NOT NULL,
    up          INTEGER NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    latency_ms  INTEGER NOT NULL DEFAULT 0
);

-- What a successful check found, where that says more than "up": the
-- records a DNS name resolved to, the message of a push.
ALTER TABLE check_results ADD COLUMN detail TEXT;
