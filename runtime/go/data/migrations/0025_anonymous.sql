-- Anonymous: a member may post or comment without their name showing. The
-- author is still stored — it's theirs to edit or delete, and moderators and
-- editors can see who wrote it — but everyone else sees "Anonymous" and nothing
-- that leads back to them (see data/anonymous.go).
ALTER TABLE posts    ADD COLUMN anonymous INTEGER NOT NULL DEFAULT 0;
ALTER TABLE comments ADD COLUMN anonymous INTEGER NOT NULL DEFAULT 0;
