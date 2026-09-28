package data

import (
	"database/sql"
	"regexp"
	"time"
)

// --- Chats & messages (community feed) ---
//
// A chat is a realtime message feed: <friendo-chat chat-id="general"> on a
// page. A chat's id is the thing the page names, so ids are slugs you can type
// (`general`, `event-42`), not generated hashes. A page that names a chat that
// doesn't exist yet makes it the first time the page is served (EnsureChat via
// ChatIDsInHTML), so an owner never has to register one by hand — and a
// template can name a chat per record (`chat-id="{{ record.slug }}"`), since
// the ids come out of the owner's own rendered markup.

// chatIDPattern is what a chat id may look like: letters, digits, `.`, `_`, `-`;
// 1–64 characters; starts with a letter or digit. Tight enough to be a URL
// segment and an HTML attribute without escaping.
var chatIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidChatID reports whether s is an acceptable chat id.
func ValidChatID(s string) bool { return chatIDPattern.MatchString(s) }

// chatTagPattern finds every <friendo-chat … chat-id="…"> in rendered HTML.
var chatTagPattern = regexp.MustCompile(`(?is)<friendo-chat\b[^>]*?\bchat-id\s*=\s*["']([^"']*)["']`)

// ChatIDsInHTML returns the distinct, valid chat ids that <friendo-chat> tags
// in a rendered page name, in order of first appearance.
func ChatIDsInHTML(html string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range chatTagPattern.FindAllStringSubmatch(html, -1) {
		id := m[1]
		if !ValidChatID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ListChats returns the site's chats (public).
func (db *DB) ListChats() ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, name, kind, created, updated FROM chats WHERE site_id = ? ORDER BY created`,
		db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, kind, created, updated string
		if err := rows.Scan(&id, &name, &kind, &created, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "kind": kind, "created": created, "updated": updated})
	}
	return out, rows.Err()
}

// CreateChat inserts a chat and returns its id. An empty id gets a generated
// one; an empty name takes the id; an empty kind is "feed". A taken id is a
// unique-constraint error from the driver — callers that want "make it if it's
// missing" use EnsureChat.
func (db *DB) CreateChat(id, name, kind string) (string, error) {
	if id == "" {
		id = GenerateID()
	}
	if name == "" {
		name = id
	}
	if kind == "" {
		kind = "feed"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO chats (id, site_id, name, kind, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, name, kind, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// EnsureChat makes the chat if no chat with that id exists yet, named after
// its id. It reports whether it made one. Ids come from the site owner's own
// markup, so an unknown id is a new chat, not a mistake.
func (db *DB) EnsureChat(id string) (bool, error) {
	if !ValidChatID(id) {
		return false, nil
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	res, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO chats (id, site_id, name, kind, created, updated) VALUES (?, ?, ?, 'feed', ?, ?)`,
		id, db.SiteID, id, now, now,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetChat returns a chat by id, or sql.ErrNoRows.
func (db *DB) GetChat(id string) (map[string]any, error) {
	var name, kind, created, updated string
	err := db.Conn.QueryRow(
		`SELECT name, kind, created, updated FROM chats WHERE id = ? AND site_id = ?`, id, db.SiteID,
	).Scan(&name, &kind, &created, &updated)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "kind": kind, "created": created, "updated": updated}, nil
}

// DeleteChat removes a chat and its messages.
func (db *DB) DeleteChat(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM chats WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	db.Conn.Exec(`DELETE FROM messages WHERE chat_id = ? AND site_id = ?`, id, db.SiteID)
	return nil
}

// messageSelect joins the author profile's display fields, like comments.
const messageSelect = `SELECT m.id, m.chat_id, m.parent_id, m.author_id, m.body, m.created,
       a.name, a.avatar
FROM messages m LEFT JOIN authors a ON a.id = m.author_id`

func scanMessage(scan func(dest ...any) error) (map[string]any, error) {
	var id, chatID, parentID, authorID, body, created string
	var authorName, authorAvatar sql.NullString
	if err := scan(&id, &chatID, &parentID, &authorID, &body, &created, &authorName, &authorAvatar); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "chat_id": chatID, "parent_id": parentID, "author_id": authorID,
		"body": body, "created": created,
		"author_name": authorName.String, "author_avatar": authorAvatar.String,
	}, nil
}

// ListMessages returns a chat's messages, oldest first. Each row carries a
// `mine` flag for the viewer (false for a visitor).
func (db *DB) ListMessages(chatID, viewerUserID string) ([]map[string]any, error) {
	q := `SELECT m.id, m.chat_id, m.parent_id, m.author_id, m.body, m.created, a.name, a.avatar,
	             CASE WHEN ? != '' AND a.user_id = ? THEN 1 ELSE 0 END AS mine
	      FROM messages m LEFT JOIN authors a ON a.id = m.author_id
	      WHERE m.site_id = ? AND m.chat_id = ? ORDER BY m.created ASC`
	rows, err := db.Conn.Query(q, viewerUserID, viewerUserID, db.SiteID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, chatID, parentID, authorID, body, created string
		var authorName, authorAvatar sql.NullString
		var mine bool
		if err := rows.Scan(&id, &chatID, &parentID, &authorID, &body, &created, &authorName, &authorAvatar, &mine); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "chat_id": chatID, "parent_id": parentID, "author_id": authorID,
			"body": body, "created": created, "mine": mine,
			"author_name": authorName.String, "author_avatar": authorAvatar.String,
		})
	}
	return out, rows.Err()
}

// CreateMessage inserts a message and returns its id.
func (db *DB) CreateMessage(chatID, parentID, authorID, body string) (string, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO messages (id, site_id, chat_id, author_id, body, parent_id, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, chatID, authorID, body, parentID, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetMessage returns a single message by id, or sql.ErrNoRows.
func (db *DB) GetMessage(id string) (map[string]any, error) {
	row := db.Conn.QueryRow(messageSelect+` WHERE m.id = ? AND m.site_id = ?`, id, db.SiteID)
	return scanMessage(row.Scan)
}

// DeleteMessage removes a message by id.
func (db *DB) DeleteMessage(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM messages WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UserOwnsMessage reports whether the account wrote the message.
func (db *DB) UserOwnsMessage(userID, messageID string) bool {
	if userID == "" {
		return false
	}
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM messages m JOIN authors a ON a.id = m.author_id
		 WHERE m.site_id = ? AND m.id = ? AND a.user_id = ? LIMIT 1`,
		db.SiteID, messageID, userID,
	).Scan(&one)
	return err == nil
}
