-- Persisted per-site settings as key/value rows. Backs admin-configurable
-- options such as comment auto-approval (moderation.auto_approve).
CREATE TABLE IF NOT EXISTS site_settings (
    site_id TEXT NOT NULL DEFAULT 'local',
    key     TEXT NOT NULL,
    value   TEXT NOT NULL DEFAULT '',
    updated TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (site_id, key)
);
