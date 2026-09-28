-- Channels are now chats: the table, the messages column, and the feature
-- switch. Same rows, same ids — only the names change.
ALTER TABLE channels RENAME TO chats;
ALTER TABLE messages RENAME COLUMN channel_id TO chat_id;
DROP INDEX IF EXISTS idx_channels_site;
DROP INDEX IF EXISTS idx_messages_channel;
CREATE INDEX IF NOT EXISTS idx_chats_site ON chats(site_id);
CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(site_id, chat_id);
UPDATE OR REPLACE site_settings SET key = 'features.chats' WHERE key = 'features.channels';
