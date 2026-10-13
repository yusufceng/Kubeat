-- What happened the last time each channel was sent to, so a silently
-- broken channel is visible where channels are managed.
ALTER TABLE notification_channels ADD COLUMN last_attempt_at INTEGER;
ALTER TABLE notification_channels ADD COLUMN last_ok INTEGER NOT NULL DEFAULT 1;
ALTER TABLE notification_channels ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
