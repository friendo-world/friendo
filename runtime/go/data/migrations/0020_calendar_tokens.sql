-- Private calendar feeds (0.6 follow-up). A member's calendar app can't sign in,
-- so their feed URL carries a secret: one token per account, shown again on
-- request and reset at will (the old link stops working). Stored like a session
-- token. The rsvps index serves "my events" (?mine=1) by persona.
CREATE TABLE IF NOT EXISTS calendar_tokens (
    user_id   TEXT PRIMARY KEY,
    site_id   TEXT NOT NULL DEFAULT 'local',
    token     TEXT NOT NULL,
    created   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    last_used TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_calendar_tokens_token ON calendar_tokens(site_id, token);
CREATE INDEX IF NOT EXISTS idx_rsvps_author ON rsvps(site_id, author_id, answer);
