-- Group chats (0.6 follow-up). A chat can belong to a group: group_id is the
-- group's post id and key is the name the page gave it (chat-id), unique within
-- the group — so two groups may both have "general". A group chat's row id is
-- derived from (group, key) so messages keep a plain chat_id. Site-wide chats
-- keep group_id '' and key = id.
ALTER TABLE chats ADD COLUMN group_id TEXT NOT NULL DEFAULT '';
ALTER TABLE chats ADD COLUMN key TEXT NOT NULL DEFAULT '';
UPDATE chats SET key = id WHERE group_id = '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_group_key ON chats(site_id, group_id, key) WHERE group_id != '';
CREATE INDEX IF NOT EXISTS idx_chats_group ON chats(site_id, group_id);
