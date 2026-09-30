package data

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A visitor is someone who hasn't signed in. When a site lets visitors react,
// vote or RSVP (the visitors_can_* settings), their first action creates a real
// account row for them — no email, role 'visitor' — with one profile, also
// marked role 'visitor'. Everything they do points at that profile exactly like
// a member's would, and the browser keeps a long-lived session for it.
//
// When the visitor signs in, the account carries over: a new email turns the
// visitor account into a member account in place (PromoteVisitor), and an email
// that already has an account takes the visitor's activity with it
// (MergeVisitor). Clearing cookies loses a visitor's activity; that's accepted.
//
// Visitors hold no capabilities and rank 0, so every role check, capGate and
// {% members only %} stays closed to them, and GetSessionUser never returns one.

// RoleVisitor is the role of a visitor's account and of its profile.
const RoleVisitor = "visitor"

// VisitorSessionTTL is how long a visitor's browser stays linked to their
// activity. It's renewed whenever they act, so an active visitor never lapses.
const VisitorSessionTTL = 365 * 24 * time.Hour

// visitorIdleAge is how long a visitor account that never did anything is
// kept before Open clears it away.
const visitorIdleAge = 30 * 24 * time.Hour

// maxVisitorName caps the name a visitor can give themselves.
const maxVisitorName = 60

// ErrNameTaken means a visitor asked for a name a member's profile already uses.
var ErrNameTaken = errors.New("that name belongs to a member — pick another")

// authorTables lists every table whose author_id points at a profile the
// visitor could have written to, and whether it has a one-per-author unique
// index (where the member's own row must win on merge).
var authorTables = []struct {
	table  string
	unique bool
}{
	{"reactions", true},
	{"poll_votes", true},
	{"rsvps", true},
	{"comments", false},
	{"posts", false},
	{"messages", false},
	{"notifications", true}, // the recipient's; its unique index dedups repeats
}

// AuthorDisplayName is how a profile's name shows beside what it wrote. A
// visitor's name is theirs to type, so it's marked — "Robin (visitor)" can't
// pass for a member called Robin — and one who gave none is just "Visitor".
func AuthorDisplayName(name, role string) string {
	if role != RoleVisitor {
		return name
	}
	if name == "" {
		return "Visitor"
	}
	return name + " (visitor)"
}

// IsVisitor reports whether the account is a visitor's.
func (u *User) IsVisitor() bool { return u != nil && u.Role == RoleVisitor }

// CreateVisitor makes a visitor account and its profile. The name may be empty:
// a visitor shows as "Visitor" until they give one.
func (db *DB) CreateVisitor() (*User, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	tx, err := db.Conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO users (id, site_id, email, name, password_hash, role, auth_methods, created, updated)
		 VALUES (?, ?, '', '', '', ?, '[]', ?, ?)`,
		id, db.SiteID, RoleVisitor, now, now,
	); err != nil {
		return nil, fmt.Errorf("creating visitor: %w", err)
	}
	// No slug: a visitor has no profile page.
	if _, err := tx.Exec(
		`INSERT INTO authors (id, site_id, user_id, name, email, role, slug, created, updated)
		 VALUES (?, ?, ?, '', '', ?, '', ?, ?)`,
		GenerateID(), db.SiteID, id, RoleVisitor, now, now,
	); err != nil {
		return nil, fmt.Errorf("creating visitor profile: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &User{ID: id, SiteID: db.SiteID, Role: RoleVisitor, AuthMethods: "[]", Created: now, Updated: now}, nil
}

// VisitorName returns the name a visitor gave themselves ("" if none).
func (db *DB) VisitorName(userID string) string {
	var name string
	db.Conn.QueryRow(
		`SELECT name FROM authors WHERE site_id = ? AND user_id = ? AND role = ? ORDER BY created LIMIT 1`,
		db.SiteID, userID, RoleVisitor,
	).Scan(&name)
	return name
}

// SetVisitorName names a visitor's profile. A name a member's profile already
// uses is refused (ErrNameTaken), so "Sam (visitor)" can't stand in for Sam.
func (db *DB) SetVisitorName(userID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len([]rune(name)) > maxVisitorName {
		return fmt.Errorf("name is too long (%d characters at most)", maxVisitorName)
	}
	var taken string
	err := db.Conn.QueryRow(
		`SELECT id FROM authors WHERE site_id = ? AND role != ? AND LOWER(name) = LOWER(?) LIMIT 1`,
		db.SiteID, RoleVisitor, name,
	).Scan(&taken)
	if err == nil {
		return ErrNameTaken
	}
	if err != sql.ErrNoRows {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.Conn.Exec(
		`UPDATE authors SET name = ?, updated = ? WHERE site_id = ? AND user_id = ? AND role = ?`,
		name, now, db.SiteID, userID, RoleVisitor,
	)
	return err
}

// PromoteVisitor turns a visitor account into a member account for an email
// no account has yet. It's the same account afterwards, so everything the
// visitor did stays theirs with nothing to move. A name the visitor gave is
// kept; otherwise the profile is named from the email, as CreateMember does.
func (db *DB) PromoteVisitor(userID, email, role string) (*User, error) {
	email = NormalizeEmail(email)
	if role == "" {
		role = "member"
	}
	name := db.VisitorName(userID)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	tx, err := db.Conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`UPDATE users SET email = ?, name = ?, role = ?, auth_methods = '["otp"]', updated = ?
		 WHERE id = ? AND site_id = ? AND role = ?`,
		email, name, role, now, userID, db.SiteID, RoleVisitor,
	)
	if err != nil {
		return nil, fmt.Errorf("promoting visitor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	if _, err := tx.Exec(
		`UPDATE authors SET name = ?, email = ?, role = 'member', slug = ?, updated = ?
		 WHERE site_id = ? AND user_id = ? AND role = ?`,
		name, email, db.uniqueSlug(Slugify(name), ""), now, db.SiteID, userID, RoleVisitor,
	); err != nil {
		return nil, fmt.Errorf("promoting visitor profile: %w", err)
	}
	// The visitor's year-long session must not outlive the promotion as a
	// member session: the caller signs the new member in afresh.
	if _, err := tx.Exec(`DELETE FROM sessions WHERE site_id = ? AND user_id = ?`, db.SiteID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetUserByID(userID)
}

// MergeVisitor moves everything a visitor did onto a member's default profile,
// then deletes the visitor account, its profile and its sessions. Where both
// did the same thing — reacted with the same emoji, voted in the same poll,
// answered the same date — the member's own row wins and the visitor's is
// dropped. All in one transaction: a merge happens entirely or not at all.
func (db *DB) MergeVisitor(visitorID, memberID string) error {
	target := db.DefaultAuthorID(memberID)
	if target == "" {
		return fmt.Errorf("the account has no profile to merge into")
	}
	tx, err := db.Conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	if err := tx.QueryRow(`SELECT role FROM users WHERE id = ? AND site_id = ?`, visitorID, db.SiteID).Scan(&role); err != nil {
		return err
	}
	if role != RoleVisitor {
		return fmt.Errorf("not a visitor account")
	}
	// Profile ids are unique across the database, so author_id alone picks the
	// visitor's rows (poll_votes has no site_id column to match on).
	from := `(SELECT id FROM authors WHERE site_id = ? AND user_id = ?)`
	for _, t := range authorTables {
		verb := "UPDATE"
		if t.unique {
			verb = "UPDATE OR IGNORE"
		}
		if _, err := tx.Exec(
			verb+` `+t.table+` SET author_id = ? WHERE author_id IN `+from,
			target, db.SiteID, visitorID,
		); err != nil {
			return fmt.Errorf("merging %s: %w", t.table, err)
		}
		// Whatever the unique index kept back was a duplicate of the member's own.
		if t.unique {
			if _, err := tx.Exec(
				`DELETE FROM `+t.table+` WHERE author_id IN `+from,
				db.SiteID, visitorID,
			); err != nil {
				return fmt.Errorf("clearing %s: %w", t.table, err)
			}
		}
	}
	for _, q := range []string{
		`DELETE FROM authors WHERE site_id = ? AND user_id = ?`,
		`DELETE FROM sessions WHERE site_id = ? AND user_id = ?`,
		`DELETE FROM users WHERE site_id = ? AND id = ?`,
	} {
		if _, err := tx.Exec(q, db.SiteID, visitorID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateVisitorSession starts a session that lasts VisitorSessionTTL.
func (db *DB) CreateVisitorSession(userID, ipAddress, userAgent string) (string, error) {
	return db.createSession(userID, ipAddress, userAgent, VisitorSessionTTL)
}

// ExtendSession pushes a session's expiry out to ttl from now.
func (db *DB) ExtendSession(token string, ttl time.Duration) {
	expiresAt := time.Now().UTC().Add(ttl).Format("2006-01-02T15:04:05Z")
	db.Conn.Exec(`UPDATE sessions SET expires_at = ? WHERE site_id = ? AND token = ?`, expiresAt, db.SiteID, token)
}

// ClearIdleVisitors deletes every expired session, and visitor accounts older
// than visitorIdleAge that have nothing to show — say, one reaction that was
// later taken back. A visitor with activity is kept (their reactions and
// answers still count) even once their browser has forgotten them. Run on every
// Open; cheap when there's nothing to clear.
func (db *DB) ClearIdleVisitors() error {
	now := time.Now().UTC()
	if _, err := db.Conn.Exec(
		`DELETE FROM sessions WHERE site_id = ? AND expires_at < ?`,
		db.SiteID, now.Format("2006-01-02T15:04:05Z"),
	); err != nil {
		return err
	}
	used := []string{}
	for _, t := range authorTables {
		used = append(used, `SELECT author_id FROM `+t.table)
	}
	rows, err := db.Conn.Query(
		`SELECT u.id FROM users u WHERE u.site_id = ? AND u.role = ? AND u.created < ?
		 AND NOT EXISTS (SELECT 1 FROM authors a WHERE a.user_id = u.id
		                 AND a.id IN (`+strings.Join(used, " UNION ")+`))`,
		db.SiteID, RoleVisitor, now.Add(-visitorIdleAge).Format("2006-01-02T15:04:05Z"),
	)
	if err != nil {
		return err
	}
	var idle []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		idle = append(idle, id)
	}
	rows.Close()
	for _, id := range idle {
		for _, q := range []string{
			`DELETE FROM authors WHERE site_id = ? AND user_id = ?`,
			`DELETE FROM sessions WHERE site_id = ? AND user_id = ?`,
			`DELETE FROM users WHERE site_id = ? AND id = ?`,
		} {
			if _, err := db.Conn.Exec(q, db.SiteID, id); err != nil {
				return err
			}
		}
	}
	return nil
}
