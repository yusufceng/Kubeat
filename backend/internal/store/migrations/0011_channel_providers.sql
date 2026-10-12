-- More notification providers. The table is rebuilt because SQLite cannot
-- drop the CHECK that limited the type; the type is validated by the server.
-- Nothing refers to this table, so the rebuild needs no special care.
--
-- secret holds the credential of providers that have one beside the target
-- (a bot token, a routing key). namespaces limits a channel to the monitors
-- of some namespaces, as a JSON array; empty means every monitor.
CREATE TABLE notification_channels_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL,
    target     TEXT    NOT NULL,
    secret     TEXT    NOT NULL DEFAULT '',
    namespaces TEXT    NOT NULL DEFAULT '[]',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

INSERT INTO notification_channels_new (id, name, type, target, enabled, created_at)
SELECT id, name, type, target, enabled, created_at FROM notification_channels;

DROP TABLE notification_channels;
ALTER TABLE notification_channels_new RENAME TO notification_channels;
