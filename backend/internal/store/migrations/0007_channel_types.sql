-- More kinds of notification channel. The column that held a webhook URL
-- becomes "target": a URL for webhook, slack and teams channels, and a
-- comma-separated list of addresses for email. SQLite cannot change a CHECK
-- constraint in place, so the table is rebuilt.
CREATE TABLE notification_channels_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL CHECK (type IN ('webhook', 'slack', 'teams', 'email')),
    target     TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

INSERT INTO notification_channels_new (id, name, type, target, enabled, created_at)
SELECT id, name, type, url, enabled, created_at FROM notification_channels;

DROP TABLE notification_channels;
ALTER TABLE notification_channels_new RENAME TO notification_channels;
