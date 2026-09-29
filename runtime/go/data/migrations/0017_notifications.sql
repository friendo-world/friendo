-- In-page notifications (v0.6 Tier D). One row per thing a member should know
-- about: someone followed you, commented on your post, asked to join your group,
-- added you to one, invited you to an event. author_id is the recipient persona
-- (an account's inbox is the union over its personas); actor_id is who did it.
-- The unique index makes writes idempotent: a follow/unfollow/follow storm is one
-- row. No email in 0.6; the shape is what a digest would read later.
CREATE TABLE IF NOT EXISTS notifications (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    author_id   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT '',
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    actor_id    TEXT NOT NULL DEFAULT '',
    read        INTEGER NOT NULL DEFAULT 0,
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_notifications_inbox ON notifications(site_id, author_id, read, created);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_dedupe ON notifications(site_id, author_id, kind, target_type, target_id, actor_id);
