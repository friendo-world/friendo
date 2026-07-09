-- File-authored polls: a poll can carry an author-chosen slug so it can be
-- declared in a post's front matter and resolved by name. The partial unique
-- index keeps slugs unique per site while still allowing many slug-less polls
-- (those created directly through the admin API).
ALTER TABLE polls ADD COLUMN slug TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_polls_slug ON polls(site_id, slug) WHERE slug != '';
