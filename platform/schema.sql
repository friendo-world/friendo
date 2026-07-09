-- Friendo platform schema (D1)
-- Platform-only tables: site registry + Better Auth.
-- Site content lives in per-site D1 databases (managed by WfP user Workers).

-- Site registry
CREATE TABLE IF NOT EXISTS sites (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    subdomain   TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    d1_id       TEXT NOT NULL DEFAULT '',   -- per-site D1 database UUID (set at provision time)
    r2_bucket   TEXT NOT NULL DEFAULT '',   -- per-site R2 bucket name (set at provision time)
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_subdomain ON sites(subdomain);

-- Site collaborators (platform-level co-owners).
-- Invited by email so someone can be added before they have a friendo.world
-- account; access activates automatically once they sign up with that email.
-- This is purely platform access (dashboard + the Admin-button SSO handoff); it
-- is separate from a site's own internal users/roles.
CREATE TABLE IF NOT EXISTS site_members (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL,              -- sites.id (= subdomain)
    email       TEXT NOT NULL,              -- invited email, lowercased
    invited_by  TEXT NOT NULL DEFAULT '',   -- user.id of the owner who invited
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_site_members ON site_members(site_id, email);
CREATE INDEX IF NOT EXISTS idx_site_members_email ON site_members(email);

-- Better Auth tables (platform accounts)

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
    id                    TEXT PRIMARY KEY,
    accountId             TEXT NOT NULL,
    providerId            TEXT NOT NULL,
    userId                TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    accessToken           TEXT,
    refreshToken          TEXT,
    idToken               TEXT,
    accessTokenExpiresAt  TEXT,
    refreshTokenExpiresAt TEXT,
    scope                 TEXT,
    password              TEXT,
    createdAt             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updatedAt             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
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
