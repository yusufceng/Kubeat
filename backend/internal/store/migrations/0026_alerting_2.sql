-- Per-channel timing of outage notifications. delay_seconds is how long an
-- outage must still be going on before the channel hears of it (0: at
-- once); repeat_seconds is the channel's own reminder interval for an
-- outage it was told of (0: UPTIME_REMINDER_INTERVAL).
ALTER TABLE notification_channels ADD COLUMN delay_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_channels ADD COLUMN repeat_seconds INTEGER NOT NULL DEFAULT 0;
-- Slow response. slow_above_ms is the monitor's threshold (0: off);
-- slow_since is since when its last failure_threshold checks have all
-- succeeded but taken longer, kept by the alert engine as results arrive,
-- NULL while the monitor responds in time. No incident is opened for it.
ALTER TABLE monitors ADD COLUMN slow_above_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE monitors ADD COLUMN slow_since INTEGER;
