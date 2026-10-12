-- Hourly summaries of check results. They outlive the raw rows, so uptime and
-- response time can be shown over months without keeping every check.
-- Latency columns cover successful checks only: a failed check's latency is
-- usually its timeout and says nothing about response time.
CREATE TABLE check_rollups (
    monitor_id        INTEGER NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    bucket_start      INTEGER NOT NULL, -- start of the hour, Unix milliseconds
    checks            INTEGER NOT NULL,
    up_checks         INTEGER NOT NULL,
    up_latency_sum_ms INTEGER NOT NULL,
    up_latency_max_ms INTEGER NOT NULL,
    PRIMARY KEY (monitor_id, bucket_start)
) WITHOUT ROWID;

CREATE INDEX idx_check_rollups_bucket ON check_rollups (bucket_start);
