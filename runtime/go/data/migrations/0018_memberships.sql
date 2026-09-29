-- Groups (v0.6 Tier C). A group is a post in the `groups` collection — its
-- settings (visibility, join rule) live in the post's data like any declared
-- field. Membership is the one new table: who belongs to which group, as what
-- (moderator | member), in what state (member | requested | invited).
CREATE TABLE IF NOT EXISTS memberships (
    id        TEXT PRIMARY KEY,
    site_id   TEXT NOT NULL DEFAULT 'local',
    group_id  TEXT NOT NULL DEFAULT '',        -- posts.id of the group record
    author_id TEXT NOT NULL DEFAULT '',        -- authors.id (persona)
    role      TEXT NOT NULL DEFAULT 'member',  -- moderator | member
    status    TEXT NOT NULL DEFAULT 'member',  -- member | requested | invited
    created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memberships_pair ON memberships(site_id, group_id, author_id);
CREATE INDEX IF NOT EXISTS idx_memberships_author ON memberships(site_id, author_id, status);
