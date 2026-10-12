-- When the certificate presented by an HTTPS target expires; NULL when the
-- check saw no certificate.
ALTER TABLE check_results ADD COLUMN cert_expires_at INTEGER;

-- An incident is either an outage or a certificate about to expire. A
-- monitor can have one open incident of each kind.
ALTER TABLE incidents ADD COLUMN kind TEXT NOT NULL DEFAULT 'down' CHECK (kind IN ('down', 'cert'));
DROP INDEX idx_incidents_open;
CREATE UNIQUE INDEX idx_incidents_open ON incidents (monitor_id, kind) WHERE resolved_at IS NULL;
