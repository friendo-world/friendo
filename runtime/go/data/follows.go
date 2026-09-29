package data

import (
	"errors"
	"time"
)

// Follows (v0.6 Tier B).
//
// One table, one word: a row says profile A follows profile B. A "friend" is a
// mutual pair — derived when read, never stored twice. Both sides are author ids
// (profiles), like every other community row, so a person with two profiles
// follows as whichever one their attribution flows to. The graph is the site's
// own: it syncs on `push --users` beside the authors and never leaves the folder.

// ErrSelfFollow is returned for a profile following itself.
var ErrSelfFollow = errors.New("you can't follow yourself")

// ToggleFollow follows followeeID as followerID, or unfollows if already
// following. Returns whether the follower now follows the followee.
func (db *DB) ToggleFollow(followerID, followeeID string) (bool, error) {
	if followerID == "" || followeeID == "" {
		return false, errors.New("follower and followee are required")
	}
	if followerID == followeeID {
		return false, ErrSelfFollow
	}
	if db.IsFollowing(followerID, followeeID) {
		return false, db.Unfollow(followerID, followeeID)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO follows (id, site_id, follower_id, followee_id, created) VALUES (?, ?, ?, ?, ?)`,
		GenerateID(), db.SiteID, followerID, followeeID, now,
	)
	return err == nil, err
}

// Unfollow removes the follow, if any.
func (db *DB) Unfollow(followerID, followeeID string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM follows WHERE site_id = ? AND follower_id = ? AND followee_id = ?`,
		db.SiteID, followerID, followeeID,
	)
	return err
}

// IsFollowing reports whether a follows b.
func (db *DB) IsFollowing(a, b string) bool {
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM follows WHERE site_id = ? AND follower_id = ? AND followee_id = ? LIMIT 1`,
		db.SiteID, a, b,
	).Scan(&one)
	return err == nil
}

func (db *DB) idList(query string, args ...any) []string {
	out := []string{}
	rows, err := db.Conn.Query(query, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// Following returns the ids of the profiles authorID follows, oldest first —
// {{ user.following }} in templates. Never nil.
func (db *DB) Following(authorID string) []string {
	if authorID == "" {
		return []string{}
	}
	return db.idList(`SELECT followee_id FROM follows WHERE site_id = ? AND follower_id = ? ORDER BY created, rowid`, db.SiteID, authorID)
}

// Followers returns the ids of the profiles following authorID, oldest first.
func (db *DB) Followers(authorID string) []string {
	if authorID == "" {
		return []string{}
	}
	return db.idList(`SELECT follower_id FROM follows WHERE site_id = ? AND followee_id = ? ORDER BY created, rowid`, db.SiteID, authorID)
}

// Friends returns the profiles authorID follows who follow back — a mutual
// follow, computed here rather than stored.
func (db *DB) Friends(authorID string) []string {
	if authorID == "" {
		return []string{}
	}
	return db.idList(
		`SELECT f.followee_id FROM follows f
		 JOIN follows b ON b.site_id = f.site_id AND b.follower_id = f.followee_id AND b.followee_id = f.follower_id
		 WHERE f.site_id = ? AND f.follower_id = ? ORDER BY f.created, f.rowid`,
		db.SiteID, authorID,
	)
}

// FollowCounts returns how many follow authorID and how many it follows.
func (db *DB) FollowCounts(authorID string) (followers, following int) {
	db.Conn.QueryRow(`SELECT COUNT(*) FROM follows WHERE site_id = ? AND followee_id = ?`, db.SiteID, authorID).Scan(&followers)
	db.Conn.QueryRow(`SELECT COUNT(*) FROM follows WHERE site_id = ? AND follower_id = ?`, db.SiteID, authorID).Scan(&following)
	return
}

// FollowerProfiles returns the profiles following authorID, oldest follow first.
func (db *DB) FollowerProfiles(authorID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT `+prefixed(profileCols, "a.")+` FROM follows f JOIN authors a ON a.id = f.follower_id
		 WHERE f.site_id = ? AND f.followee_id = ? ORDER BY f.created, f.rowid`,
		db.SiteID, authorID,
	)
	if err != nil {
		return nil, err
	}
	return scanProfiles(rows)
}

// FollowingProfiles returns the profiles authorID follows, oldest follow first.
func (db *DB) FollowingProfiles(authorID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT `+prefixed(profileCols, "a.")+` FROM follows f JOIN authors a ON a.id = f.followee_id
		 WHERE f.site_id = ? AND f.follower_id = ? ORDER BY f.created, f.rowid`,
		db.SiteID, authorID,
	)
	if err != nil {
		return nil, err
	}
	return scanProfiles(rows)
}

// prefixed turns "a, b, c" into "p.a, p.b, p.c" for a joined select.
func prefixed(cols, prefix string) string {
	out := ""
	for i, c := range splitCols(cols) {
		if i > 0 {
			out += ", "
		}
		out += prefix + c
	}
	return out
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, r := range cols {
		switch r {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ':
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// ListFollows returns every follow on the site (for sync).
func (db *DB) ListFollows() ([]map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT id, follower_id, followee_id, created FROM follows WHERE site_id = ? ORDER BY created, rowid`, db.SiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, follower, followee, created string
		if err := rows.Scan(&id, &follower, &followee, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "follower_id": follower, "followee_id": followee, "created": created})
	}
	return out, rows.Err()
}

// UpsertFollow inserts a follow by pair (for sync); an existing pair is left alone.
func (db *DB) UpsertFollow(id, followerID, followeeID, created string) error {
	if followerID == "" || followeeID == "" || followerID == followeeID {
		return nil
	}
	if id == "" {
		id = GenerateID()
	}
	if created == "" {
		created = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	_, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO follows (id, site_id, follower_id, followee_id, created) VALUES (?, ?, ?, ?, ?)`,
		id, db.SiteID, followerID, followeeID, created,
	)
	return err
}

// DeleteFollowsForAuthor removes every follow a profile is on either side of.
func (db *DB) DeleteFollowsForAuthor(authorID string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM follows WHERE site_id = ? AND (follower_id = ? OR followee_id = ?)`,
		db.SiteID, authorID, authorID,
	)
	return err
}
