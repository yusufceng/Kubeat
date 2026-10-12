-- When a notification about the incident was last sent, so that reminders
-- for a long outage can be spaced out. NULL for incidents from before
-- reminders existed.
ALTER TABLE incidents ADD COLUMN last_notified_at INTEGER;
