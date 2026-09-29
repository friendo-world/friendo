-- Profiles (v0.6 Tier A): an author is a public persona with an address. Every
-- author gets a slug (backfilled from the name on open), a short bio, and a data
-- JSON blob for the fields a site declares under [profiles] fields — the same
-- lever posts got in 0003. The unique index is partial so legacy rows with an
-- empty slug don't collide before the backfill runs.
ALTER TABLE authors ADD COLUMN slug TEXT NOT NULL DEFAULT '';
ALTER TABLE authors ADD COLUMN bio  TEXT NOT NULL DEFAULT '';
ALTER TABLE authors ADD COLUMN data TEXT NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX IF NOT EXISTS idx_authors_slug ON authors(site_id, slug) WHERE slug != '';

-- Follows (Tier B): one-way, one row per pair, author ids on both sides. A
-- friend is a mutual pair, derived at read time, never stored twice.
CREATE TABLE IF NOT EXISTS follows (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    follower_id TEXT NOT NULL DEFAULT '',
    followee_id TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_follows_pair ON follows(site_id, follower_id, followee_id);
CREATE INDEX IF NOT EXISTS idx_follows_followee ON follows(site_id, followee_id);
