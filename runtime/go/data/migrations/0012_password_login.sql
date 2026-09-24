-- v0.5: sites sign in with an emailed code by default; passwords are opt-in via
-- the access.password_login setting. A site upgraded from an earlier version
-- has people who only know a password, so seed the setting ON wherever a real
-- password exists — nobody gets locked out by the upgrade. Fresh sites (no
-- users yet) get nothing here and default to code-only.
INSERT OR IGNORE INTO site_settings (site_id, key, value, updated)
SELECT DISTINCT site_id, 'access.password_login', 'true', strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM users
WHERE password_hash != '';
