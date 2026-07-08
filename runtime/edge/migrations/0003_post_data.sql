-- Arbitrary per-record fields for file-based (Hugo-style) content authoring.
-- Front matter keys that aren't dedicated columns are stored here as JSON and
-- exposed to templates as record.data.<field>.
ALTER TABLE posts ADD COLUMN data TEXT NOT NULL DEFAULT '{}';
