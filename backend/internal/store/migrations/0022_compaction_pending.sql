-- A compaction the database is owed: recorded before the secrets of a
-- database written without a key are encrypted, and removed once VACUUM
-- and the emptying of the write-ahead log are done. A start cut short in
-- between leaves it here, and the next start compacts. attempts counts the
-- starts in a row that tried and did not finish; past a bound a start
-- skips the compaction, so that one too long for the time a start is given
-- cannot keep the backend down. One row at most.
CREATE TABLE compaction_pending (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    since    INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0
);
