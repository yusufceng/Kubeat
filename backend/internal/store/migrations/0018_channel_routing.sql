-- Channels choose what they hear about: monitors by tag, and which events.
ALTER TABLE notification_channels ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE notification_channels ADD COLUMN events TEXT NOT NULL DEFAULT '[]';
