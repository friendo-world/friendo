-- Friendo edge runtime schema (D1)
-- Identical to the Go runtime's SQLite schema.
-- Apply with: wrangler d1 execute DB --file=schema.sql

-- Content
CREATE TABLE IF NOT EXISTS posts (
    id           TEXT PRIMARY KEY,
    site_id      TEXT NOT NULL DEFAULT 'default',
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
    site_id   TEXT NOT NULL DEFAULT 'default',
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
    site_id     TEXT NOT NULL DEFAULT 'default',
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    author_id   TEXT NOT NULL DEFAULT '',
    emoji       TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_reactions_target ON reactions(site_id, target_type, target_id);

-- Auth
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    site_id       TEXT NOT NULL DEFAULT 'default',
    email         TEXT NOT NULL DEFAULT '',
    phone         TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    avatar        TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'member',
    auth_methods  TEXT NOT NULL DEFAULT '["password"]',
    created       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_site_email ON users(site_id, email);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL DEFAULT 'default',
    user_id    TEXT NOT NULL DEFAULT '',
    token      TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token ON sessions(site_id, token);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
