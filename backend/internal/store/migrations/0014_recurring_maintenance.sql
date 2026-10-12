-- Maintenance windows that come back. repeat is '' for a window that
-- happens once, or 'daily', 'weekly' or 'monthly'; starts_at and ends_at
-- are then the first occurrence. Later ones are at the same time of day in
-- timezone (an IANA name), so that a window at 02:00 stays at 02:00 when
-- the clocks change.
ALTER TABLE maintenance_windows ADD COLUMN repeat TEXT NOT NULL DEFAULT '';
ALTER TABLE maintenance_windows ADD COLUMN timezone TEXT NOT NULL DEFAULT '';
