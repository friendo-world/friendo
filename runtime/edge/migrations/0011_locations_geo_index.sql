-- Index locations by (site, type, lat, lng) so the aggregate map query — every
-- published post's pin, optionally within a viewport bbox — stays fast as a
-- site accumulates geo-tags. Mirrors the Go runtime.
CREATE INDEX IF NOT EXISTS idx_locations_geo ON locations(site_id, target_type, lat, lng);
