package data

import (
	"database/sql"
	"time"
)

// Notifications (v0.6 Tier D): the in-page inbox every social feature needs.
//
// A row is "profile X should know that actor Y did <kind> to <target>". Rows are
// written by the follow, comment, group and invitation handlers and read by
// <friendo-inbox>, the badge in <friendo-auth>, and {{ user.unread }}. They never
// sync or export — like comments and RSVPs, they're the live site's own — and
// there's no switch of their own: the inbox is on while follows or groups is.

// Notification kinds.
const (
	NotifyFollow      = "follow"       // actor followed you (target: author)
	NotifyComment     = "comment"      // actor commented on your post (target: comment)
	NotifyJoinRequest = "join_request" // actor asked to join your group (target: post)
	NotifyMembership  = "membership"   // you were added to / approved for a group (target: post)
	NotifyEventInvite = "event_invite" // actor invited you to an event (target: post)
)

// NotificationsOn reports whether the inbox is in use: it rides on the follows
// and groups switches rather than having one of its own.
func (db *DB) NotificationsOn() bool {
	return db.FeatureOn("follows") || db.FeatureOn("groups")
}

// authorUserID returns the account a profile belongs to ("" if none).
func (db *DB) authorUserID(authorID string) string {
	var uid string
	db.Conn.QueryRow(`SELECT user_id FROM authors WHERE site_id = ? AND id = ?`, db.SiteID, authorID).Scan(&uid)
	return uid
}

// Notify writes one notification for a recipient profile. It skips an empty
// recipient and anything a person did to themselves (any of their own profiles),
// and a repeat of the same (recipient, kind, target, actor) is a no-op — so
// nothing a member does twice piles up in someone's inbox.
func (db *DB) Notify(recipientAuthorID, kind, targetType, targetID, actorAuthorID string) error {
	if recipientAuthorID == "" || kind == "" {
		return nil
	}
	if actorAuthorID != "" {
		if ru, au := db.authorUserID(recipientAuthorID), db.authorUserID(actorAuthorID); ru != "" && ru == au {
			return nil
		}
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO notifications (id, site_id, author_id, kind, target_type, target_id, actor_id, read, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		GenerateID(), db.SiteID, recipientAuthorID, kind, targetType, targetID, actorAuthorID, now,
	)
	return err
}

// NotifyMany writes the same notification to several recipients.
func (db *DB) NotifyMany(recipients []string, kind, targetType, targetID, actorAuthorID string) {
	for _, r := range recipients {
		db.Notify(r, kind, targetType, targetID, actorAuthorID)
	}
}

// Notification is one inbox row, with the actor's profile attached.
type Notification struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	ActorID    string         `json:"from_id"`
	Actor      map[string]any `json:"from"` // profile map, or nil
	Read       bool           `json:"read"`
	Created    string         `json:"created"`
	// Target is filled in by the API from the type + id: {type, id, title, url, …}.
	Target map[string]any `json:"target"`
}

// ListNotifications returns an account's inbox — every profile's rows together,
// newest first, at most limit (50 when 0). unreadOnly narrows to unread.
func (db *DB) ListNotifications(userID string, unreadOnly bool, limit int) ([]*Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT n.id, n.kind, n.target_type, n.target_id, n.actor_id, n.read, n.created
	      FROM notifications n JOIN authors a ON a.id = n.author_id AND a.site_id = n.site_id
	      WHERE n.site_id = ? AND a.user_id = ?`
	if unreadOnly {
		q += ` AND n.read = 0`
	}
	q += ` ORDER BY n.created DESC, n.rowid DESC LIMIT ?`
	rows, err := db.Conn.Query(q, db.SiteID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Notification{}
	actorIDs := map[string]bool{}
	for rows.Next() {
		n := &Notification{}
		var read int
		if err := rows.Scan(&n.ID, &n.Kind, &n.TargetType, &n.TargetID, &n.ActorID, &read, &n.Created); err != nil {
			return nil, err
		}
		n.Read = read == 1
		out = append(out, n)
		if n.ActorID != "" {
			actorIDs[n.ActorID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// One query for every actor's profile — a visitor's too, marked "(visitor)",
	// since a visitor's approved comment still tells the post's author.
	if len(actorIDs) > 0 {
		rows, err := db.Conn.Query(`SELECT `+profileCols+` FROM authors WHERE site_id = ?`, db.SiteID)
		var profiles []map[string]any
		if err == nil {
			profiles, err = scanProfiles(rows)
		}
		if err == nil {
			byID := map[string]map[string]any{}
			for _, p := range profiles {
				byID[p["id"].(string)] = p
			}
			for _, n := range out {
				n.Actor = byID[n.ActorID]
			}
		}
	}
	return out, nil
}

// UnreadCount is the number of unread notifications across an account's profiles.
func (db *DB) UnreadCount(userID string) int {
	var n int
	db.Conn.QueryRow(
		`SELECT COUNT(*) FROM notifications n JOIN authors a ON a.id = n.author_id AND a.site_id = n.site_id
		 WHERE n.site_id = ? AND a.user_id = ? AND n.read = 0`,
		db.SiteID, userID,
	).Scan(&n)
	return n
}

// MarkRead marks one of an account's notifications read; sql.ErrNoRows if it
// isn't theirs.
func (db *DB) MarkRead(userID, id string) error {
	res, err := db.Conn.Exec(
		`UPDATE notifications SET read = 1 WHERE site_id = ? AND id = ?
		   AND author_id IN (SELECT id FROM authors WHERE site_id = ? AND user_id = ?)`,
		db.SiteID, id, db.SiteID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkAllRead marks every notification of an account read.
func (db *DB) MarkAllRead(userID string) error {
	_, err := db.Conn.Exec(
		`UPDATE notifications SET read = 1 WHERE site_id = ? AND read = 0
		   AND author_id IN (SELECT id FROM authors WHERE site_id = ? AND user_id = ?)`,
		db.SiteID, db.SiteID, userID,
	)
	return err
}

// DeleteNotificationsForAuthor drops a profile's inbox and anything it did.
func (db *DB) DeleteNotificationsForAuthor(authorID string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM notifications WHERE site_id = ? AND (author_id = ? OR actor_id = ?)`,
		db.SiteID, authorID, authorID,
	)
	return err
}
