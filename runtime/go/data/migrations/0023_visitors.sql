-- Visitors: someone who reacts, votes or RSVPs without signing in gets a real
-- account row with no email (role 'visitor') and a profile marked role
-- 'visitor', so everything they do points at an author id like a member's
-- would. Signing in later links or merges that account (see data/visitors.go).
-- Many visitors share the empty email, so the one-email-per-account rule only
-- applies to accounts that have one — the same partial-index shape as the
-- profile slug index in 0016.
DROP INDEX IF EXISTS idx_users_site_email;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_site_email ON users(site_id, email) WHERE email != '';
CREATE INDEX IF NOT EXISTS idx_users_role ON users(site_id, role);
