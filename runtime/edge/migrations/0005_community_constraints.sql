-- Community write constraints (reactions + polls).
-- One reaction per (target, author, emoji): re-reacting is a toggle, not a
-- duplicate. One vote per (poll, author): a member votes at most once.
CREATE UNIQUE INDEX IF NOT EXISTS idx_reactions_unique
    ON reactions(site_id, target_type, target_id, author_id, emoji);
CREATE UNIQUE INDEX IF NOT EXISTS idx_poll_votes_unique
    ON poll_votes(poll_id, author_id);
