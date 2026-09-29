package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// chatTagPattern finds every <friendo-chat …> tag in rendered HTML; the
// attribute patterns read its chat-id and (optional) group, in any order.
var (
	chatTagPattern  = regexp.MustCompile(`(?is)<friendo-chat\b([^>]*)>`)
	chatIDAttrRe    = regexp.MustCompile(`(?i)\bchat-id\s*=\s*["']([^"']*)["']`)
	chatGroupAttrRe = regexp.MustCompile(`(?i)\bgroup\s*=\s*["']([^"']*)["']`)
)

// ChatRef is what one <friendo-chat> tag names: a chat key and, for a group's
// chat, the group's slug (<friendo-chat chat-id="general" group="board">).
type ChatRef struct {
	Key   string
	Group string // "" for a site-wide chat
}

// ChatRefsInHTML returns the distinct, valid chats that <friendo-chat> tags in
// a rendered page name, in order of first appearance.
func ChatRefsInHTML(html string) []ChatRef {
	var out []ChatRef
	seen := map[ChatRef]bool{}
	for _, m := range chatTagPattern.FindAllStringSubmatch(html, -1) {
		attrs := m[1]
		idm := chatIDAttrRe.FindStringSubmatch(attrs)
		if idm == nil || !ValidChatID(idm[1]) {
			continue
		}
		ref := ChatRef{Key: idm[1]}
		if gm := chatGroupAttrRe.FindStringSubmatch(attrs); gm != nil {
			if !ValidChatID(gm[1]) {
				continue
			}
			ref.Group = gm[1]
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// ChatIDsInHTML returns the distinct, valid site-wide chat ids a rendered page
// names (group chats aside), in order of first appearance.
func ChatIDsInHTML(html string) []string {
	var out []string
	for _, ref := range ChatRefsInHTML(html) {
		if ref.Group == "" {
			out = append(out, ref.Key)
		}
	}
	return out
}

// GroupChatID derives a group chat's row id from the group and the key, so
// registering it twice is one row and messages keep a plain chat_id.
func GroupChatID(groupID, key string) string {
	sum := sha256.Sum256([]byte("chat\n" + groupID + "\n" + key))
	return hex.EncodeToString(sum[:])[:24]
}

const chatCols = `id, name, kind, group_id, key, created, updated`

// ChatRowID is the chats row id behind a chat map: the key-derived hash for a
// group's chat, the id itself for a site chat.
func ChatRowID(chat map[string]any) string {
	id, _ := chat["id"].(string)
	if g, _ := chat["group_id"].(string); g != "" {
		return GroupChatID(g, id)
	}
	return id
}

func scanChats(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, kind, groupID, key, created, updated string
		if err := rows.Scan(&id, &name, &kind, &groupID, &key, &created, &updated); err != nil {
			return nil, err
		}
		// A group's chat goes by its key ("general"), unique within the group;
		// the row's hash id is ChatRowID's business. A site chat's id is its key.
		if groupID != "" {
			id = key
		}
		out = append(out, map[string]any{"id": id, "name": name, "kind": kind, "group_id": groupID, "created": created, "updated": updated})
	}
	return out, rows.Err()
}

// ListChats returns the site's own chats (public) — a group's chats are the
// group's, listed under it.
func (db *DB) ListChats() ([]map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+chatCols+` FROM chats WHERE site_id = ? AND group_id = '' ORDER BY created`, db.SiteID)
	if err != nil {
		return nil, err
	}
	return scanChats(rows)
}

// ListGroupChats returns a group's chats, oldest first.
func (db *DB) ListGroupChats(groupID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+chatCols+` FROM chats WHERE site_id = ? AND group_id = ? ORDER BY created, rowid`, db.SiteID, groupID)
	if err != nil {
		return nil, err
	}
	return scanChats(rows)
}

// GetGroupChat returns one of a group's chats by key, or sql.ErrNoRows.
func (db *DB) GetGroupChat(groupID, key string) (map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+chatCols+` FROM chats WHERE site_id = ? AND group_id = ? AND key = ? LIMIT 1`, db.SiteID, groupID, key)
	if err != nil {
		return nil, err
	}
	list, err := scanChats(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// EnsureGroupChat makes a group's chat if the group has none with that key,
// named after the key. It reports whether it made one.
func (db *DB) EnsureGroupChat(groupID, key string) (bool, error) {
	if groupID == "" || !ValidChatID(key) {
		return false, nil
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	res, err := db.Conn.Exec(
		`INSERT OR IGNORE INTO chats (id, site_id, name, kind, group_id, key, created, updated) VALUES (?, ?, ?, 'feed', ?, ?, ?, ?)`,
		GroupChatID(groupID, key), db.SiteID, key, groupID, key, now, now,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CreateGroupChat adds a chat to a group by hand (a moderator's act). A taken
// key is a unique-constraint error from the driver.
func (db *DB) CreateGroupChat(groupID, key, name string) (string, error) {
	if name == "" {
		name = key
	}
	id := GroupChatID(groupID, key)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO chats (id, site_id, name, kind, group_id, key, created, updated) VALUES (?, ?, ?, 'feed', ?, ?, ?, ?)`,
		id, db.SiteID, name, groupID, key, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// DeleteChatsForGroup removes a group's chats and their messages (the group was deleted).
func (db *DB) DeleteChatsForGroup(groupID string) error {
	if groupID == "" {
		return nil
	}
	chats, err := db.ListGroupChats(groupID)
	if err != nil {
		return err
	}
	for _, c := range chats {
		db.DeleteChat(ChatRowID(c))
	}
	return nil
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
		`INSERT INTO chats (id, site_id, name, kind, key, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, name, kind, id, now, now,
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
		`INSERT OR IGNORE INTO chats (id, site_id, name, kind, key, created, updated) VALUES (?, ?, ?, 'feed', ?, ?, ?)`,
		id, db.SiteID, id, id, now, now,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetChat returns a chat by id (site-wide or a group's), or sql.ErrNoRows.
func (db *DB) GetChat(id string) (map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT `+chatCols+` FROM chats WHERE id = ? AND site_id = ? LIMIT 1`, id, db.SiteID)
	if err != nil {
		return nil, err
	}
	list, err := scanChats(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
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
