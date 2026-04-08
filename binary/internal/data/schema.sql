-- Friendo common schema
-- Applied automatically on first run. Identical structure locally (SQLite) and on the edge (D1).

-- Core
CREATE TABLE IF NOT EXISTS sites (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    subdomain   TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    config_json TEXT NOT NULL DEFAULT '{}',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Content
CREATE TABLE IF NOT EXISTS posts (
    id           TEXT PRIMARY KEY,
    site_id      TEXT NOT NULL DEFAULT 'local',
    collection   TEXT NOT NULL DEFAULT 'posts',
    slug         TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',
    author_id    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'draft',
    published_at TEXT NOT NULL DEFAULT '',
    created      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_posts_site_collection ON posts(site_id, collection);
CREATE INDEX IF NOT EXISTS idx_posts_slug ON posts(site_id, collection, slug);

CREATE TABLE IF NOT EXISTS comments (
    id        TEXT PRIMARY KEY,
    site_id   TEXT NOT NULL DEFAULT 'local',
    post_id   TEXT NOT NULL DEFAULT '',
    parent_id TEXT NOT NULL DEFAULT '',
    author_id TEXT NOT NULL DEFAULT '',
    body      TEXT NOT NULL DEFAULT '',
    created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_comments_post ON comments(site_id, post_id);

CREATE TABLE IF NOT EXISTS reactions (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    author_id   TEXT NOT NULL DEFAULT '',
    emoji       TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_reactions_target ON reactions(site_id, target_type, target_id);

-- Community
CREATE TABLE IF NOT EXISTS channels (
    id      TEXT PRIMARY KEY,
    site_id TEXT NOT NULL DEFAULT 'local',
    name    TEXT NOT NULL DEFAULT '',
    kind    TEXT NOT NULL DEFAULT 'feed',
    created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_channels_site ON channels(site_id);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL DEFAULT 'local',
    channel_id TEXT NOT NULL DEFAULT '',
    author_id  TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    parent_id  TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(site_id, channel_id);

CREATE TABLE IF NOT EXISTS polls (
    id        TEXT PRIMARY KEY,
    site_id   TEXT NOT NULL DEFAULT 'local',
    post_id   TEXT NOT NULL DEFAULT '',
    question  TEXT NOT NULL DEFAULT '',
    options   TEXT NOT NULL DEFAULT '[]',
    closes_at TEXT NOT NULL DEFAULT '',
    created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_polls_post ON polls(site_id, post_id);

CREATE TABLE IF NOT EXISTS poll_votes (
    id           TEXT PRIMARY KEY,
    poll_id      TEXT NOT NULL DEFAULT '',
    option_index INTEGER NOT NULL DEFAULT 0,
    author_id    TEXT NOT NULL DEFAULT '',
    created      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_poll_votes_poll ON poll_votes(poll_id);

-- People
CREATE TABLE IF NOT EXISTS authors (
    id      TEXT PRIMARY KEY,
    site_id TEXT NOT NULL DEFAULT 'local',
    name    TEXT NOT NULL DEFAULT '',
    email   TEXT NOT NULL DEFAULT '',
    avatar  TEXT NOT NULL DEFAULT '',
    role    TEXT NOT NULL DEFAULT 'member',
    created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_authors_site ON authors(site_id);

-- Geo
CREATE TABLE IF NOT EXISTS locations (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    lat         REAL NOT NULL DEFAULT 0,
    lng         REAL NOT NULL DEFAULT 0,
    label       TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_locations_target ON locations(site_id, target_type, target_id);

-- Media
CREATE TABLE IF NOT EXISTS files (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    record_type TEXT NOT NULL DEFAULT '',
    record_id   TEXT NOT NULL DEFAULT '',
    field       TEXT NOT NULL DEFAULT '',
    r2_key      TEXT NOT NULL DEFAULT '',
    mime        TEXT NOT NULL DEFAULT '',
    size        INTEGER NOT NULL DEFAULT 0,
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_files_record ON files(site_id, record_type, record_id);

-- Admin auth
CREATE TABLE IF NOT EXISTS admin (
    id            TEXT PRIMARY KEY DEFAULT 'admin',
    password_hash TEXT NOT NULL DEFAULT '',
    created       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
