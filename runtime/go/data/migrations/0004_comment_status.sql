-- Comment moderation: every comment carries a moderation status. New comments
-- default to 'pending' and only become publicly visible once an admin approves
-- them. The index serves both the public (approved) read and the admin queue.
ALTER TABLE comments ADD COLUMN status TEXT NOT NULL DEFAULT 'pending';
CREATE INDEX IF NOT EXISTS idx_comments_post_status ON comments(site_id, post_id, status);
