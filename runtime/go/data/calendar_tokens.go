package data

import (
	"database/sql"
	"time"
)

// Calendar tokens: the secret in a member's private feed URL
// (/calendar.ics?token=…). One per account; asking again shows the same one;
// resetting mints a new one and the old link stops working. Never synced —
// like sessions, it's the live site's own.

// CalendarToken returns the account's feed token, making one the first time.
func (db *DB) CalendarToken(userID string) (string, error) {
	if userID == "" {
		return "", sql.ErrNoRows
	}
	var token string
	err := db.Conn.QueryRow(`SELECT token FROM calendar_tokens WHERE site_id = ? AND user_id = ?`, db.SiteID, userID).Scan(&token)
	if err == nil {
		return token, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if _, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO calendar_tokens (user_id, site_id, token, created) VALUES (?, ?, ?, ?)`,
		userID, db.SiteID, generateToken(), now,
	); err != nil {
		return "", err
	}
	err = db.Conn.QueryRow(`SELECT token FROM calendar_tokens WHERE site_id = ? AND user_id = ?`, db.SiteID, userID).Scan(&token)
	return token, err
}

// ResetCalendarToken replaces the account's feed token; the old one is gone.
func (db *DB) ResetCalendarToken(userID string) (string, error) {
	if userID == "" {
		return "", sql.ErrNoRows
	}
	token := generateToken()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO calendar_tokens (user_id, site_id, token, created, last_used) VALUES (?, ?, ?, ?, '')
		 ON CONFLICT(user_id) DO UPDATE SET token = excluded.token, created = excluded.created, last_used = ''`,
		userID, db.SiteID, token, now,
	)
	return token, err
}

// UserByCalendarToken returns the account a feed token belongs to (and notes
// the use), or sql.ErrNoRows for a token nobody holds.
func (db *DB) UserByCalendarToken(token string) (*User, error) {
	if token == "" {
		return nil, sql.ErrNoRows
	}
	var userID string
	if err := db.Conn.QueryRow(`SELECT user_id FROM calendar_tokens WHERE site_id = ? AND token = ?`, db.SiteID, token).Scan(&userID); err != nil {
		return nil, err
	}
	db.Conn.Exec(`UPDATE calendar_tokens SET last_used = ? WHERE site_id = ? AND user_id = ?`, time.Now().UTC().Format("2006-01-02T15:04:05Z"), db.SiteID, userID)
	return db.GetUserByID(userID)
}
