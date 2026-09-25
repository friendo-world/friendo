-- Calendar: a post's `when`. One row per series (a one-off is a series with no
-- rule); occurrences are expanded on read, never stored. starts/ends are local
-- wall-clock times with the offset of that instant (RFC 3339); the IANA zone
-- name travels beside them so daylight-saving is re-derived per occurrence.
-- Keyed by target like locations, so a post's row has a deterministic id
-- (DeterministicEventID) and re-importing content/ upserts in place.
CREATE TABLE IF NOT EXISTS events (
    id          TEXT PRIMARY KEY,
    site_id     TEXT NOT NULL DEFAULT 'local',
    target_type TEXT NOT NULL DEFAULT 'post',
    target_id   TEXT NOT NULL DEFAULT '',
    starts      TEXT NOT NULL DEFAULT '',
    ends        TEXT NOT NULL DEFAULT '',
    all_day     INTEGER NOT NULL DEFAULT 0,
    timezone    TEXT NOT NULL DEFAULT '',
    rrule       TEXT NOT NULL DEFAULT '',
    exdates     TEXT NOT NULL DEFAULT '[]',
    created     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_events_target ON events(site_id, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_events_starts ON events(site_id, starts);
