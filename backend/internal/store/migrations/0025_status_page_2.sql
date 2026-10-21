-- Status pages, second round: sections, membership rules, updates with a
-- history, and the settings of a page.

-- Settings. noindex asks search engines to stay away; hide_paused leaves
-- paused monitors off the page; language is "" for the instance default,
-- else "tr" or "en"; detail_pages lets a monitor be opened on a page of its
-- own; password_hash is the bcrypt hash of the visitors' password, empty for
-- a page open to all.
ALTER TABLE status_pages ADD COLUMN noindex       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE status_pages ADD COLUMN hide_paused   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE status_pages ADD COLUMN language      TEXT    NOT NULL DEFAULT '';
ALTER TABLE status_pages ADD COLUMN detail_pages  INTEGER NOT NULL DEFAULT 1;
ALTER TABLE status_pages ADD COLUMN password_hash TEXT    NOT NULL DEFAULT '';

-- The named groups of a page, in order. A monitor or a rule without one is
-- in the unnamed group at the top. Sections are rewritten with the page, so
-- their ids are not stable.
CREATE TABLE status_page_sections (
    id       INTEGER PRIMARY KEY,
    page_id  INTEGER NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL,
    UNIQUE (page_id, name)
);

-- A monitor's section; NULL for the unnamed top group.
ALTER TABLE status_page_monitors ADD COLUMN section_id INTEGER REFERENCES status_page_sections (id) ON DELETE SET NULL;

-- Membership rules: the monitors of a namespace ('' for any) that carry a
-- tag ('' for any) appear in the rule's section, after the explicit
-- entries. They are evaluated when the page is served, so monitors found by
-- discovery appear by themselves.
CREATE TABLE status_page_rules (
    id         INTEGER PRIMARY KEY,
    page_id    INTEGER NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
    section_id INTEGER REFERENCES status_page_sections (id) ON DELETE SET NULL,
    namespace  TEXT    NOT NULL DEFAULT '',
    tag        TEXT    NOT NULL DEFAULT '',
    position   INTEGER NOT NULL
);

CREATE INDEX idx_status_page_rules_page ON status_page_rules (page_id, position);

-- Updates: what the page tells its visitors over time, newest first. kind
-- is investigating, identified, monitoring, resolved, notice or
-- maintenance; incident_id, when set, is the incident the update is about,
-- and is cleared when the incident goes. created_by is the username of the
-- writer, as it was.
CREATE TABLE status_page_updates (
    id          INTEGER PRIMARY KEY,
    page_id     INTEGER NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
    incident_id INTEGER REFERENCES incidents (id) ON DELETE SET NULL,
    kind        TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    body        TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    created_by  TEXT    NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL
);

CREATE INDEX idx_status_page_updates_page ON status_page_updates (page_id, created_at);
-- For the view, which lists the notes of the monitors a page shows.
CREATE INDEX idx_incident_notes_shown ON incident_notes (show_on_status_page, created_at);
