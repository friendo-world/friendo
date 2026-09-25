-- RSVP: a member's answer (going / not_going / maybe) to one occurrence of an
-- event. event_id is the series (events.id); occurrence is the RFC 3339 start
-- of the instance answered for — a one-off uses its own start — so on a weekly
-- event "are you coming?" means *this* Tuesday. One answer per member per
-- occurrence; changing your mind is an update.
CREATE TABLE IF NOT EXISTS rsvps (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL DEFAULT 'local',
    event_id   TEXT NOT NULL DEFAULT '',
    occurrence TEXT NOT NULL DEFAULT '',
    author_id  TEXT NOT NULL DEFAULT '',
    answer     TEXT NOT NULL DEFAULT 'going',
    created    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_rsvps_unique ON rsvps(event_id, occurrence, author_id);
CREATE INDEX IF NOT EXISTS idx_rsvps_event ON rsvps(site_id, event_id, occurrence, answer);
