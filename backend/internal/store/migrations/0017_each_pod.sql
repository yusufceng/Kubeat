-- Whether a monitor discovered from a service checks every pod behind it.
ALTER TABLE monitors ADD COLUMN each_pod INTEGER NOT NULL DEFAULT 0;
