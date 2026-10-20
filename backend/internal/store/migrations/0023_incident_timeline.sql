-- What the incident page tells: when Kubeat opened the incident, whom it
-- told and with what outcome, and what people noted on it.

-- The check that reached the failure threshold and so opened the incident;
-- started_at is the first failure of that streak, which comes earlier.
-- NULL for incidents opened before this was recorded.
ALTER TABLE incidents ADD COLUMN opened_at INTEGER;

-- Every attempt to tell a channel about an incident, as it ended after its
-- retries. A grouped message is one row per incident it was about. The
-- channel's name and type are copies and its id has no reference, so that
-- the record outlives the channel; the error is redacted before it is
-- written, while the channel's address and secret are known to redact it
-- with. The monitor is the incident's own, taken from it on insert.
CREATE TABLE incident_deliveries (
    id           INTEGER PRIMARY KEY,
    incident_id  INTEGER NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    monitor_id   INTEGER NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    channel_id   INTEGER NOT NULL,
    channel_name TEXT    NOT NULL,
    channel_type TEXT    NOT NULL,
    -- What the message reported (down, up, reminder, cert_expiring,
    -- cert_renewed) and when that happened, as Kubeat saw it.
    event        TEXT    NOT NULL,
    event_at     INTEGER NOT NULL,
    -- How many monitors the message was about; more than one if grouped.
    group_size   INTEGER NOT NULL DEFAULT 1,
    -- When the attempt ended, whether the channel took the message, and
    -- why not when it did not.
    at           INTEGER NOT NULL,
    delivered    INTEGER NOT NULL,
    error        TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_incident_deliveries_incident ON incident_deliveries (incident_id, at);
-- For the retention cleanup, which deletes by age.
CREATE INDEX idx_incident_deliveries_at ON incident_deliveries (at);

-- Notes people leave on an incident: "rolled back at 14:05", "known: the
-- database migration". author is the username when it was written;
-- author_id the account, cleared when the account is deleted, after which
-- only administrators may change the note. show_on_status_page lets the
-- status pages showing the monitor show the note.
CREATE TABLE incident_notes (
    id                  INTEGER PRIMARY KEY,
    incident_id         INTEGER NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    author_id           INTEGER REFERENCES users (id) ON DELETE SET NULL,
    author              TEXT    NOT NULL,
    text                TEXT    NOT NULL,
    show_on_status_page INTEGER NOT NULL DEFAULT 0,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX idx_incident_notes_incident ON incident_notes (incident_id, created_at);
