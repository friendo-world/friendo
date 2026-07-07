-- Author profiles: link display profiles to accounts (users), give every
-- existing account a default profile, and remap existing content from the
-- account id to the profile id. author_id now references authors.id everywhere.
ALTER TABLE authors ADD COLUMN user_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_authors_user ON authors(user_id);

INSERT INTO authors (id, site_id, user_id, name, email, avatar, role, created, updated)
SELECT lower(hex(randomblob(12))), u.site_id, u.id, u.name, u.email, u.avatar, 'member',
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM authors a WHERE a.user_id = u.id);

UPDATE posts SET author_id = (SELECT a.id FROM authors a WHERE a.user_id = posts.author_id)
WHERE author_id != '' AND EXISTS (SELECT 1 FROM authors a WHERE a.user_id = posts.author_id);
