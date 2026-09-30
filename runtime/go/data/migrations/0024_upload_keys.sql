-- Upload keys: a drop-box post has no author, so nobody "owns" it to add images.
-- Creating one hands back a one-time key instead; only its hash is kept, it's
-- tied to the post (never to a person), and it expires within the hour.
CREATE TABLE IF NOT EXISTS upload_keys (
    post_id    TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL DEFAULT 'local',
    key_hash   TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL DEFAULT ''
);
