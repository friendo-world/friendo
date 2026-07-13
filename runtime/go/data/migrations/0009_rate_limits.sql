-- Fixed-window rate limiting (login brute-force, comment spam). Each bucket is a
-- key like "login-fail:<email>" or "comment:<author_id>" with a rolling window.
CREATE TABLE IF NOT EXISTS rate_limits (
    site_id      TEXT NOT NULL DEFAULT 'local',
    bucket       TEXT NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    window_start TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (site_id, bucket)
);
