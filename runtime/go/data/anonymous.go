package data

import "strings"

// Anonymous posts and comments: a member chose not to show their name. The
// author is still stored — the post or comment stays theirs to edit or delete —
// and moderators and editors can see who wrote it. Everyone else gets
// "Anonymous" and nothing that leads back: no author id, no profile, no
// avatar, and the post or comment is left off its author's profile page.
//
// Posts are hidden where they reach templates and exports (AttachAuthors, so a
// by_author or by_following filter, or post.author_id == user.profile_id, finds
// nothing). Comments are hidden as they're read (ListCommentsByPost, and
// ListCommentsForViewer unless the viewer may see authors).

// AnonymousName is what an anonymous post or comment shows for its author.
const AnonymousName = "Anonymous"

// SetPostAnonymous marks a post anonymous or not.
func (db *DB) SetPostAnonymous(id string, on bool) error {
	_, err := db.Conn.Exec(`UPDATE posts SET anonymous = ? WHERE id = ? AND site_id = ?`, boolInt(on), id, db.SiteID)
	return err
}

// SetCommentAnonymous marks a comment anonymous or not.
func (db *DB) SetCommentAnonymous(id string, on bool) error {
	_, err := db.Conn.Exec(`UPDATE comments SET anonymous = ? WHERE id = ? AND site_id = ?`, boolInt(on), id, db.SiteID)
	return err
}

// PostIsAnonymous reports whether a post hides its author.
func (db *DB) PostIsAnonymous(id string) bool {
	var n int
	db.Conn.QueryRow(`SELECT anonymous FROM posts WHERE id = ? AND site_id = ?`, id, db.SiteID).Scan(&n)
	return n == 1
}

// anonymousPostIDs is the set of this site's anonymous posts among ids.
func (db *DB) anonymousPostIDs(ids []string) map[string]bool {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out
	}
	args := []any{db.SiteID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := db.Conn.Query(
		`SELECT id FROM posts WHERE site_id = ? AND anonymous = 1 AND id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`,
		args...,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// HideAnonymousAuthors blanks the author of every anonymous post among records
// (author_id "", author nil) and marks it anonymous: true. AttachAuthors calls
// it, so every path that shows posts to the public gets it.
func (db *DB) HideAnonymousAuthors(records []map[string]any) {
	ids := make([]string, 0, len(records))
	for _, r := range records {
		if id, _ := r["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	anon := db.anonymousPostIDs(ids)
	for _, r := range records {
		id, _ := r["id"].(string)
		r["anonymous"] = anon[id]
		if anon[id] {
			r["author_id"] = ""
			r["author"] = nil
		}
	}
}

// HideCommentAuthor blanks an anonymous comment's author for someone who may
// not see it.
func HideCommentAuthor(c map[string]any) {
	if c["anonymous"] != true {
		return
	}
	c["author_id"] = ""
	c["author_name"] = AnonymousName
	c["author_avatar"] = ""
	c["visitor"] = false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
