-- Status pages: a public view of some monitors, without sign-in.
CREATE TABLE status_pages (
    id           INTEGER PRIMARY KEY,
    -- The last part of the page's address.
    slug         TEXT    NOT NULL UNIQUE,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    -- A notice shown at the top of the page; empty for none.
    announcement TEXT    NOT NULL DEFAULT '',
    -- An unpublished page is not served.
    published    INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL
);

CREATE TABLE status_page_monitors (
    page_id      INTEGER NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
    monitor_id   INTEGER NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    -- The name the page shows instead of the monitor's own; empty for that.
    display_name TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (page_id, monitor_id)
) WITHOUT ROWID;

CREATE INDEX idx_status_page_monitors_monitor ON status_page_monitors (monitor_id);
