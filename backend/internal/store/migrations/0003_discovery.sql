-- Where a monitor came from: the namespace it belongs to and, for monitors
-- created by discovery, the object it was created from. Both are empty for
-- monitors that were entered by hand.
ALTER TABLE monitors ADD COLUMN namespace TEXT NOT NULL DEFAULT '';
ALTER TABLE monitors ADD COLUMN source TEXT NOT NULL DEFAULT '';

-- A discovered object is monitored at most once.
CREATE UNIQUE INDEX idx_monitors_source ON monitors (namespace, source) WHERE source != '';
