-- Friendo common schema for Cloudflare D1
-- Identical to the local SQLite schema (binary/internal/data/schema.sql).
-- Applied once when the D1 database is created.

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
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_subdomain ON sites(subdomain);

-- Content
CREATE TABLE IF NOT EXISTS posts (
    id           TEXT PRIMARY KEY,
    site_id      TEXT NOT NULL,
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
    site_id   TEXT NOT NULL,
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
    site_id     TEXT NOT NULL,
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
    site_id TEXT NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    kind    TEXT NOT NULL DEFAULT 'feed',
    created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_channels_site ON channels(site_id);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL,
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
    site_id   TEXT NOT NULL,
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
    site_id TEXT NOT NULL,
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
    site_id     TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    lat         REAL NOT NULL DEFAULT 0,
    lng         REAL NOT NULL DEFAULT 0,
    label       TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_locations_target ON locations(site_id, target_type, target_id);

-- Auth (Better Auth tables)
CREATE TABLE IF NOT EXISTS "user" (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL DEFAULT '',
    email          TEXT NOT NULL,
    emailVerified  INTEGER NOT NULL DEFAULT 0,
    image          TEXT,
    createdAt      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updatedAt      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_email ON "user"(email);

CREATE TABLE IF NOT EXISTS "session" (
    id        TEXT PRIMARY KEY,
    expiresAt TEXT NOT NULL,
    token     TEXT NOT NULL,
    ipAddress TEXT,
    userAgent TEXT,
    userId    TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    createdAt TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updatedAt TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_token ON "session"(token);
CREATE INDEX IF NOT EXISTS idx_session_userId ON "session"(userId);

CREATE TABLE IF NOT EXISTS "account" (
    id                  TEXT PRIMARY KEY,
    accountId           TEXT NOT NULL,
    providerId          TEXT NOT NULL,
    userId              TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    accessToken         TEXT,
    refreshToken        TEXT,
    idToken             TEXT,
    accessTokenExpiresAt TEXT,
    refreshTokenExpiresAt TEXT,
    scope               TEXT,
    password            TEXT,
    createdAt           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updatedAt           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_account_userId ON "account"(userId);

CREATE TABLE IF NOT EXISTS "verification" (
    id         TEXT PRIMARY KEY,
    identifier TEXT NOT NULL,
    value      TEXT NOT NULL,
    expiresAt  TEXT NOT NULL,
    createdAt  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updatedAt  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Media
CREATE TABLE IF NOT EXISTS files (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL,
    record_type TEXT NOT NULL DEFAULT '',
    record_id   TEXT NOT NULL DEFAULT '',
    field       TEXT NOT NULL DEFAULT '',
    r2_key      TEXT NOT NULL DEFAULT '',
    mime        TEXT NOT NULL DEFAULT '',
    size        INTEGER NOT NULL DEFAULT 0,
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_files_record ON files(site_id, record_type, record_id);
