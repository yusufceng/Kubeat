-- Per-monitor options of HTTP checks. They are ignored for TCP monitors.
ALTER TABLE monitors ADD COLUMN http_method TEXT NOT NULL DEFAULT 'GET' CHECK (http_method IN ('GET', 'HEAD'));
-- Accepted status codes as a list of codes and ranges, e.g. "200-299,401".
ALTER TABLE monitors ADD COLUMN http_expected_status TEXT NOT NULL DEFAULT '200-399';
-- Text the response body must contain; empty means the body is not inspected.
ALTER TABLE monitors ADD COLUMN http_keyword TEXT NOT NULL DEFAULT '';
-- Extra request headers as a JSON object of name to value.
ALTER TABLE monitors ADD COLUMN http_headers TEXT NOT NULL DEFAULT '{}';
ALTER TABLE monitors ADD COLUMN tls_skip_verify INTEGER NOT NULL DEFAULT 0;
