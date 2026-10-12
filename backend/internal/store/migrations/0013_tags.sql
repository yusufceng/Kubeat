-- Free-form labels on a monitor, as a JSON array of strings: "prod",
-- "team-payments". They are few and short, so they live with the monitor
-- instead of in tables of their own.
ALTER TABLE monitors ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
