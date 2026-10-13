-- Cluster objects whose annotation asks for a monitor that someone deleted
-- in Kubeat: the sync leaves them alone until they are restored or the
-- annotation goes.
CREATE TABLE dismissed_sources (
    namespace    TEXT NOT NULL,
    source       TEXT NOT NULL,
    dismissed_at INTEGER NOT NULL,
    dismissed_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (namespace, source)
);
