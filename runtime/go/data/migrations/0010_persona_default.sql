-- A member can keep several author profiles (personas); default_author_id points
-- at the one their comments/messages attribute to. Empty = fall back to the
-- earliest profile. Set via POST /_/api/me/personas/:id/default.
ALTER TABLE users ADD COLUMN default_author_id TEXT NOT NULL DEFAULT '';
