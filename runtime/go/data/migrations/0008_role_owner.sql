-- Rename the top site role from 'superadmin' to 'owner'. Co-owners are allowed;
-- the app enforces a last-owner guard so a site always has at least one owner.
-- Also index posts by author so ownership-scoped queries (edit-own, own-post
-- comment moderation) stay fast as content grows.
UPDATE users SET role = 'owner' WHERE role = 'superadmin';
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(site_id, author_id);
