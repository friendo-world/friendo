package data

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// decodeData parses a record's `data` JSON column into a map for template access
// (record.data.<field>). Empty or invalid JSON yields an empty map.
func decodeData(raw string) map[string]any {
	if raw == "" || raw == "{}" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}

//go:embed schema.sql
var schemaSQL string

//go:embed migrations/*.sql
var migrationFiles embed.FS

// DB wraps a SQLite connection for the Friendo data layer.
type DB struct {
	Conn   *sql.DB
	SiteID string
}

// Open creates or opens the SQLite database at <siteDir>/data/friendo.db,
// applies the common schema if needed, and returns a DB handle.
func Open(siteDir string) (*DB, error) {
	dataDir := filepath.Join(siteDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "friendo.db")
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	return &DB{Conn: conn, SiteID: "local"}, nil
}

type migrationDef struct {
	id   int
	name string
	sql  string
}

// allMigrations returns the ordered migration list: the baseline schema (id 1)
// followed by migrations/NNNN_*.sql.
func allMigrations() []migrationDef {
	defs := []migrationDef{{id: 1, name: "baseline", sql: schemaSQL}}

	entries, _ := fs.ReadDir(migrationFiles, "migrations")
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, _ := migrationFiles.ReadFile("migrations/" + n)
		id, _ := strconv.Atoi(strings.SplitN(n, "_", 2)[0])
		defs = append(defs, migrationDef{id: id, name: n, sql: string(b)})
	}
	return defs
}

// runMigrations applies migrations not yet recorded in schema_migrations, each
// in its own transaction.
func runMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (id INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}

	applied := map[int]bool{}
	rows, err := conn.Query(`SELECT id FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		applied[id] = true
	}
	rows.Close()

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	for _, m := range allMigrations() {
		if applied[m.id] {
			continue
		}
		tx, err := conn.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range SplitStatements(m.sql) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d (%s): %w", m.id, m.name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (id, name, applied_at) VALUES (?, ?, ?)`, m.id, m.name, now); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements drops full-line comments and splits SQL into statements.
// SplitStatements drops full-line comments and splits a multi-statement SQL string
// (schema / migration) into individual statements. Its behavior is pinned by
// tests/splitter-cases.json. Filtering full-line comments before splitting on ";"
// keeps a comment-led file (schema.sql opens with a comment) from folding its
// leading comment into — and dropping — the first real statement.
func SplitStatements(sqlText string) []string {
	var kept []string
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	var out []string
	for _, part := range strings.Split(strings.Join(kept, "\n"), ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.Conn.Close()
}

// QueryCollection returns all posts with the given collection name as
// a slice of maps for injection into a template context.
func (db *DB) QueryCollection(collection string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, slug, title, body, author_id, status, published_at, created, updated, data
		 FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`,
		db.SiteID, collection,
	)
	if err != nil {
		return nil, fmt.Errorf("querying collection %q: %w", collection, err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, slug, title, body, authorID, status, publishedAt, created, updated, data string
		if err := rows.Scan(&id, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated, &data); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{
			"id":           id,
			"slug":         slug,
			"title":        title,
			"body":         body,
			"author_id":    authorID,
			"status":       status,
			"published_at": publishedAt,
			"created":      created,
			"updated":      updated,
			"data":         decodeData(data),
		})
	}
	return results, rows.Err()
}

// scanCollectionRows maps post rows (in QueryCollection's column order) to the
// template/record shape.
func scanCollectionRows(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	var results []map[string]any
	for rows.Next() {
		var id, slug, title, body, authorID, status, publishedAt, created, updated, data string
		if err := rows.Scan(&id, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated, &data); err != nil {
			return nil, err
		}
		results = append(results, map[string]any{
			"id": id, "slug": slug, "title": title, "body": body,
			"author_id": authorID, "status": status, "published_at": publishedAt,
			"created": created, "updated": updated, "data": decodeData(data),
		})
	}
	return results, rows.Err()
}

const collectionCols = `id, slug, title, body, author_id, status, published_at, created, updated, data`

// QueryPublishedCollection returns only published posts of a collection — the
// public render path uses this so drafts and pending posts never leak.
func (db *DB) QueryPublishedCollection(collection string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT `+collectionCols+` FROM posts
		 WHERE site_id = ? AND collection = ? AND status = 'published' ORDER BY created DESC`,
		db.SiteID, collection,
	)
	if err != nil {
		return nil, fmt.Errorf("querying collection %q: %w", collection, err)
	}
	return scanCollectionRows(rows)
}

// QueryCollectionOwnedBy returns a collection's posts authored by the given
// account (any of its profiles) — the admin content list for a Contributor.
func (db *DB) QueryCollectionOwnedBy(collection, userID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT p.id, p.slug, p.title, p.body, p.author_id, p.status, p.published_at, p.created, p.updated, p.data
		 FROM posts p JOIN authors a ON a.id = p.author_id
		 WHERE p.site_id = ? AND p.collection = ? AND a.user_id = ? ORDER BY p.created DESC`,
		db.SiteID, collection, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying owned collection %q: %w", collection, err)
	}
	return scanCollectionRows(rows)
}

// ListRecordsByStatus returns posts across all collections with the given status
// (the post-review queue), newest first, with each author's display name joined.
func (db *DB) ListRecordsByStatus(status string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT p.id, p.collection, p.slug, p.title, p.author_id, p.status, p.created, a.name
		 FROM posts p LEFT JOIN authors a ON a.id = p.author_id
		 WHERE p.site_id = ? AND p.status = ? ORDER BY p.created DESC`,
		db.SiteID, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, collection, slug, title, authorID, st, created string
		var authorName sql.NullString
		if err := rows.Scan(&id, &collection, &slug, &title, &authorID, &st, &created, &authorName); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "collection": collection, "slug": slug, "title": title,
			"author_id": authorID, "author_name": authorName.String, "status": st, "created": created,
		})
	}
	return out, rows.Err()
}

// SetRecordStatus changes only a post's status (used to publish/unpublish from
// the review queue without touching its content).
func (db *DB) SetRecordStatus(id, status string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	res, err := db.Conn.Exec(
		`UPDATE posts SET status = ?, updated = ? WHERE id = ? AND site_id = ?`,
		status, now, id, db.SiteID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// QueryCollectionByField returns a single post from the given collection
// where fieldName == value (used for dynamic routing, e.g. slug lookup).
func (db *DB) QueryCollectionByField(collection, fieldName, value string) (map[string]any, error) {
	// Only allow safe field names for the WHERE clause.
	allowed := map[string]bool{"id": true, "slug": true, "title": true}
	if !allowed[fieldName] {
		return nil, fmt.Errorf("field %q not allowed for lookup", fieldName)
	}

	query := fmt.Sprintf(
		`SELECT id, slug, title, body, author_id, status, published_at, created, updated, data
		 FROM posts WHERE site_id = ? AND collection = ? AND %s = ? LIMIT 1`, fieldName,
	)
	row := db.Conn.QueryRow(query, db.SiteID, collection, value)

	var id, slug, title, body, authorID, status, publishedAt, created, updated, data string
	if err := row.Scan(&id, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated, &data); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no %s found with %s=%s", collection, fieldName, value)
		}
		return nil, err
	}

	return map[string]any{
		"id":           id,
		"slug":         slug,
		"title":        title,
		"body":         body,
		"author_id":    authorID,
		"status":       status,
		"published_at": publishedAt,
		"created":      created,
		"updated":      updated,
		"data":         decodeData(data),
	}, nil
}

// ListCollections returns the distinct collection names that have posts.
func (db *DB) ListCollections() ([]string, error) {
	rows, err := db.Conn.Query(
		`SELECT DISTINCT collection FROM posts WHERE site_id = ? ORDER BY collection`,
		db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// CollectionCount is a collection name with its record count.
type CollectionCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CollectionCounts returns the record count for each collection that has posts.
func (db *DB) CollectionCounts() ([]CollectionCount, error) {
	rows, err := db.Conn.Query(
		`SELECT collection, COUNT(*) FROM posts WHERE site_id = ? GROUP BY collection ORDER BY collection`,
		db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts []CollectionCount
	for rows.Next() {
		var c CollectionCount
		if err := rows.Scan(&c.Name, &c.Count); err != nil {
			return nil, err
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

// GetRecordByID returns a single post by id, scoped to the site.
func (db *DB) GetRecordByID(id string) (map[string]any, error) {
	row := db.Conn.QueryRow(
		`SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated, data
		 FROM posts WHERE id = ? AND site_id = ?`,
		id, db.SiteID,
	)
	var rid, collection, slug, title, body, authorID, status, publishedAt, created, updated, data string
	if err := row.Scan(&rid, &collection, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated, &data); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return map[string]any{
		"id":           rid,
		"collection":   collection,
		"slug":         slug,
		"title":        title,
		"body":         body,
		"author_id":    authorID,
		"status":       status,
		"published_at": publishedAt,
		"created":      created,
		"updated":      updated,
		"data":         decodeData(data),
	}, nil
}

// CreateRecord inserts a new post and returns its id.
func (db *DB) CreateRecord(collection, slug, title, body, status, authorID string) (string, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO posts (id, site_id, collection, slug, title, body, status, author_id, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, collection, slug, title, body, status, authorID, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateRecord updates an existing post's editable fields.
func (db *DB) UpdateRecord(id, slug, title, body, status string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	res, err := db.Conn.Exec(
		`UPDATE posts SET slug = ?, title = ?, body = ?, status = ?, updated = ?
		 WHERE id = ? AND site_id = ?`,
		slug, title, body, status, now, id, db.SiteID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// contentID derives a stable record id from (collection, slug). File-based content
// uses it so re-importing the same file always produces the same id — otherwise a
// rebuilt local database mints fresh random ids, and `push --data` (which upserts
// by id) would *add* duplicates on the target instead of replacing.
func contentID(collection, slug string) string {
	sum := sha256.Sum256([]byte(collection + "\n" + slug))
	return hex.EncodeToString(sum[:])[:24]
}

// UpsertRecordBySlug inserts or updates a record identified by (collection, slug).
// Used by the file-based content importer — unlike CreateRecord/UpdateRecord it
// also sets published_at and the arbitrary `data` JSON blob, and uses a stable
// (collection, slug)-derived id. Returns the record id.
func (db *DB) UpsertRecordBySlug(collection, slug, title, body, status, publishedAt, data string) (string, error) {
	if data == "" {
		data = "{}"
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	var id string
	err := db.Conn.QueryRow(
		`SELECT id FROM posts WHERE site_id = ? AND collection = ? AND slug = ? LIMIT 1`,
		db.SiteID, collection, slug,
	).Scan(&id)

	if err == sql.ErrNoRows {
		id = contentID(collection, slug)
		_, err = db.Conn.Exec(
			`INSERT INTO posts (id, site_id, collection, slug, title, body, status, author_id, published_at, data, created, updated)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			id, db.SiteID, collection, slug, title, body, status, publishedAt, data, now, now,
		)
		return id, err
	}
	if err != nil {
		return "", err
	}

	_, err = db.Conn.Exec(
		`UPDATE posts SET title = ?, body = ?, status = ?, published_at = ?, data = ?, updated = ?
		 WHERE id = ? AND site_id = ?`,
		title, body, status, publishedAt, data, now, id, db.SiteID,
	)
	return id, err
}

// DeleteRecord removes a post by id, scoped to the site.
func (db *DB) DeleteRecord(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM posts WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Comments ---

// commentJSON is the shape returned for a comment, with its author profile's
// display fields joined in.
func scanComment(scan func(dest ...any) error) (map[string]any, error) {
	var id, postID, parentID, authorID, body, status, created string
	var authorName, authorAvatar sql.NullString
	if err := scan(&id, &postID, &parentID, &authorID, &body, &status, &created, &authorName, &authorAvatar); err != nil {
		return nil, err
	}
	return map[string]any{
		"id":            id,
		"post_id":       postID,
		"parent_id":     parentID,
		"author_id":     authorID,
		"author_name":   authorName.String,
		"author_avatar": authorAvatar.String,
		"body":          body,
		"status":        status,
		"created":       created,
	}, nil
}

const commentSelect = `SELECT c.id, c.post_id, c.parent_id, c.author_id, c.body, c.status, c.created,
       a.name, a.avatar
FROM comments c LEFT JOIN authors a ON a.id = c.author_id`

// ListCommentsByPost returns a post's comments, oldest first. When
// includeUnapproved is false (public reads) only approved comments are returned.
func (db *DB) ListCommentsByPost(postID string, includeUnapproved bool) ([]map[string]any, error) {
	q := commentSelect + ` WHERE c.site_id = ? AND c.post_id = ?`
	args := []any{db.SiteID, postID}
	if !includeUnapproved {
		q += ` AND c.status = 'approved'`
	}
	q += ` ORDER BY c.created ASC`
	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		c, err := scanComment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCommentsForViewer returns a post's comments as seen by a given viewer.
// Everyone sees approved comments; a moderator (canModerate) also sees pending/
// rejected; an authenticated author also sees their own unapproved comments. Each
// row carries a `mine` flag (the viewer wrote it) for inline self-delete.
func (db *DB) ListCommentsForViewer(postID, viewerUserID string, canModerate bool) ([]map[string]any, error) {
	q := `SELECT c.id, c.post_id, c.parent_id, c.author_id, c.body, c.status, c.created,
	             a.name, a.avatar, CASE WHEN ? != '' AND a.user_id = ? THEN 1 ELSE 0 END AS mine
	      FROM comments c LEFT JOIN authors a ON a.id = c.author_id
	      WHERE c.site_id = ? AND c.post_id = ?`
	args := []any{viewerUserID, viewerUserID, db.SiteID, postID}
	if !canModerate {
		q += ` AND (c.status = 'approved' OR (? != '' AND a.user_id = ?))`
		args = append(args, viewerUserID, viewerUserID)
	}
	q += ` ORDER BY c.created ASC`

	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, pid, parentID, authorID, body, status, created string
		var authorName, authorAvatar sql.NullString
		var mine int
		if err := rows.Scan(&id, &pid, &parentID, &authorID, &body, &status, &created, &authorName, &authorAvatar, &mine); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "post_id": pid, "parent_id": parentID, "author_id": authorID,
			"author_name": authorName.String, "author_avatar": authorAvatar.String,
			"body": body, "status": status, "created": created, "mine": mine == 1,
		})
	}
	return out, rows.Err()
}

// ListCommentsByStatus returns all comments across the site with the given
// status (the moderation queue), newest first.
func (db *DB) ListCommentsByStatus(status string) ([]map[string]any, error) {
	q := commentSelect + ` WHERE c.site_id = ?`
	args := []any{db.SiteID}
	if status != "" {
		q += ` AND c.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY c.created DESC`
	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		c, err := scanComment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCommentsByStatusForOwner returns comments with the given status that sit
// on posts the account authored — the moderation queue for comment.moderate.own.
func (db *DB) ListCommentsByStatusForOwner(status, userID string) ([]map[string]any, error) {
	q := commentSelect + `
		JOIN posts p ON p.id = c.post_id
		JOIN authors pa ON pa.id = p.author_id
		WHERE c.site_id = ? AND pa.user_id = ?`
	args := []any{db.SiteID, userID}
	if status != "" {
		q += ` AND c.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY c.created DESC`
	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		c, err := scanComment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetComment returns a single comment by id, or sql.ErrNoRows.
func (db *DB) GetComment(id string) (map[string]any, error) {
	row := db.Conn.QueryRow(commentSelect+` WHERE c.id = ? AND c.site_id = ?`, id, db.SiteID)
	return scanComment(row.Scan)
}

// CreateComment inserts a comment (status 'pending') and returns its id.
func (db *DB) CreateComment(postID, parentID, authorID, body, status string) (string, error) {
	if status == "" {
		status = "pending"
	}
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO comments (id, site_id, post_id, parent_id, author_id, body, status, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, postID, parentID, authorID, body, status, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateCommentStatus changes a comment's moderation status.
func (db *DB) UpdateCommentStatus(id, status string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	res, err := db.Conn.Exec(
		`UPDATE comments SET status = ?, updated = ? WHERE id = ? AND site_id = ?`,
		status, now, id, db.SiteID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteComment removes a comment by id, scoped to the site.
func (db *DB) DeleteComment(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM comments WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Files (per-record media) ---

// CreateFile records an uploaded file linked to a record, returning its id.
func (db *DB) CreateFile(recordType, recordID, field, r2Key, mime string, size int64) (string, error) {
	return db.CreateFileWithID(GenerateID(), recordType, recordID, field, r2Key, mime, size)
}

// CreateFileWithID is CreateFile with a caller-supplied id, used when the id is
// needed up front (e.g. to name the stored object before the row is written).
func (db *DB) CreateFileWithID(id, recordType, recordID, field, r2Key, mime string, size int64) (string, error) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO files (id, site_id, record_type, record_id, field, r2_key, mime, size, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, recordType, recordID, field, r2Key, mime, size, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func fileJSON(id, recordType, recordID, field, r2Key, mime string, size int64, created string) map[string]any {
	return map[string]any{
		"id": id, "record_type": recordType, "record_id": recordID, "field": field,
		"url": "/" + r2Key, "r2_key": r2Key, "mime": mime, "size": size, "created": created,
	}
}

// ListFiles returns the files attached to a record (public — URLs are public).
func (db *DB) ListFiles(recordType, recordID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, record_type, record_id, field, r2_key, mime, size, created
		 FROM files WHERE site_id = ? AND record_type = ? AND record_id = ? ORDER BY created`,
		db.SiteID, recordType, recordID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, rt, rid, field, key, mime, created string
		var size int64
		if err := rows.Scan(&id, &rt, &rid, &field, &key, &mime, &size, &created); err != nil {
			return nil, err
		}
		out = append(out, fileJSON(id, rt, rid, field, key, mime, size, created))
	}
	return out, rows.Err()
}

// GetFile returns a single file's r2_key (and metadata), or sql.ErrNoRows.
func (db *DB) GetFile(id string) (map[string]any, error) {
	var rt, rid, field, key, mime, created string
	var size int64
	err := db.Conn.QueryRow(
		`SELECT record_type, record_id, field, r2_key, mime, size, created FROM files WHERE id = ? AND site_id = ?`,
		id, db.SiteID,
	).Scan(&rt, &rid, &field, &key, &mime, &size, &created)
	if err != nil {
		return nil, err
	}
	return fileJSON(id, rt, rid, field, key, mime, size, created), nil
}

// AllFiles returns every file row for the site as raw column maps — used by
// sync (pull) so deployed sites learn which media belongs to which record.
func (db *DB) AllFiles() ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, record_type, record_id, field, r2_key, mime, size, created
		 FROM files WHERE site_id = ? ORDER BY created`, db.SiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, rt, rid, field, key, mime, created string
		var size int64
		if err := rows.Scan(&id, &rt, &rid, &field, &key, &mime, &size, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "record_type": rt, "record_id": rid, "field": field,
			"r2_key": key, "mime": mime, "size": size, "created": created,
		})
	}
	return out, rows.Err()
}

// UpsertFile inserts or updates a file row by id — the sync (push/pull) writer.
func (db *DB) UpsertFile(id, recordType, recordID, field, r2Key, mime string, size int64, created string) error {
	if created == "" {
		created = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	_, err := db.Conn.Exec(
		`INSERT INTO files (id, site_id, record_type, record_id, field, r2_key, mime, size, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   record_type=excluded.record_type, record_id=excluded.record_id, field=excluded.field,
		   r2_key=excluded.r2_key, mime=excluded.mime, size=excluded.size`,
		id, db.SiteID, recordType, recordID, field, r2Key, mime, size, created)
	return err
}

// DeleteFile removes a file row and returns its r2_key so the caller can delete
// the stored object.
func (db *DB) DeleteFile(id string) (string, error) {
	var key string
	if err := db.Conn.QueryRow(`SELECT r2_key FROM files WHERE id = ? AND site_id = ?`, id, db.SiteID).Scan(&key); err != nil {
		return "", err
	}
	_, err := db.Conn.Exec(`DELETE FROM files WHERE id = ? AND site_id = ?`, id, db.SiteID)
	return key, err
}

// ListFilesByField returns a record's files with a given field (e.g. "gallery"),
// oldest first — the render helper behind record.gallery.
func (db *DB) ListFilesByField(recordType, recordID, field string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, record_type, record_id, field, r2_key, mime, size, created
		 FROM files WHERE site_id = ? AND record_type = ? AND record_id = ? AND field = ? ORDER BY created`,
		db.SiteID, recordType, recordID, field,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, rt, rid, f, key, mime, created string
		var size int64
		if err := rows.Scan(&id, &rt, &rid, &f, &key, &mime, &size, &created); err != nil {
			return nil, err
		}
		out = append(out, fileJSON(id, rt, rid, f, key, mime, size, created))
	}
	return out, rows.Err()
}

// DeleteFilesByField removes all of a record's files with the given field. Used by
// content import to reconcile a page bundle's gallery before re-importing it.
func (db *DB) DeleteFilesByField(recordType, recordID, field string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM files WHERE site_id = ? AND record_type = ? AND record_id = ? AND field = ?`,
		db.SiteID, recordType, recordID, field,
	)
	return err
}

// DeterministicFileID derives a stable file id from a record id and a source name,
// mirroring contentID for records. Same (record, name) → same id across builds and
// machines, so re-import and re-push upsert in place instead of duplicating.
func DeterministicFileID(recordID, name string) string {
	sum := sha256.Sum256([]byte(recordID + "\n" + name))
	return hex.EncodeToString(sum[:])[:24]
}

// --- Locations (geo-tagging) ---

// BBox is a geographic bounding box (a map viewport). A nil *BBox means "no
// bound" — return every point of the requested type.
type BBox struct {
	MinLat, MinLng, MaxLat, MaxLng float64
}

// PermalinkFunc maps a collection record to its public URL, derived from the
// site's page routes. It returns "" when the collection has no public page.
// The runtime builds this from its route table (see server.permalinkResolver);
// the data layer never computes URLs itself.
type PermalinkFunc func(collection string, fields map[string]string) string

// ListLocationsByType returns published locations of a target type across all
// posts — the "everything on one map" query behind an id-less <friendo-map>.
// It joins posts so each marker can carry its post's slug/title/collection (for
// a link back) and so unpublished posts stay hidden from the public map. A
// non-nil bbox limits the result to a viewport. Only post targets are joined;
// posts are the only geo-taggable type with a public URL.
func (db *DB) ListLocationsByType(targetType string, bbox *BBox) ([]map[string]any, error) {
	q := `SELECT l.id, l.target_type, l.target_id, l.lat, l.lng, l.label, l.created,
	             p.collection, p.slug, p.title
	      FROM locations l
	      JOIN posts p ON p.id = l.target_id AND p.site_id = l.site_id
	      WHERE l.site_id = ? AND l.target_type = ? AND p.status = 'published'`
	args := []any{db.SiteID, targetType}
	if bbox != nil {
		q += ` AND l.lat BETWEEN ? AND ? AND l.lng BETWEEN ? AND ?`
		args = append(args, bbox.MinLat, bbox.MaxLat, bbox.MinLng, bbox.MaxLng)
	}
	q += ` ORDER BY l.created`
	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, tt, tid, label, created, collection, slug, title string
		var lat, lng float64
		if err := rows.Scan(&id, &tt, &tid, &lat, &lng, &label, &created, &collection, &slug, &title); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "target_type": tt, "target_id": tid,
			"lat": lat, "lng": lng, "label": label, "created": created,
			"collection": collection, "slug": slug, "title": title,
		})
	}
	return out, rows.Err()
}

// ListLocations returns the locations attached to a target (public).
func (db *DB) ListLocations(targetType, targetID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, target_type, target_id, lat, lng, label, created
		 FROM locations WHERE site_id = ? AND target_type = ? AND target_id = ? ORDER BY created`,
		db.SiteID, targetType, targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, tt, tid, label, created string
		var lat, lng float64
		if err := rows.Scan(&id, &tt, &tid, &lat, &lng, &label, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "target_type": tt, "target_id": tid,
			"lat": lat, "lng": lng, "label": label, "created": created,
		})
	}
	return out, rows.Err()
}

// DeterministicLocationID derives a stable id for a post's content-declared
// location (from front matter). Same post → same id across builds, so re-import
// upserts the pin in place instead of duplicating it, and it never collides with
// API-created pins (which use random ids).
func DeterministicLocationID(recordID string) string {
	sum := sha256.Sum256([]byte(recordID + "\nlocation"))
	return hex.EncodeToString(sum[:])[:24]
}

// UpsertLocation inserts or updates a location by id — used by content import to
// reconcile a post's front-matter location without duplicating on every build.
func (db *DB) UpsertLocation(id, targetType, targetID string, lat, lng float64, label string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO locations (id, site_id, target_type, target_id, lat, lng, label, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   target_type=excluded.target_type, target_id=excluded.target_id,
		   lat=excluded.lat, lng=excluded.lng, label=excluded.label`,
		id, db.SiteID, targetType, targetID, lat, lng, label, now,
	)
	return err
}

// CreateLocation attaches a location to a target and returns its id.
func (db *DB) CreateLocation(targetType, targetID string, lat, lng float64, label string) (string, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO locations (id, site_id, target_type, target_id, lat, lng, label, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, targetType, targetID, lat, lng, label, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetLocation returns a single location by id, or sql.ErrNoRows.
func (db *DB) GetLocation(id string) (map[string]any, error) {
	var tt, tid, label, created string
	var lat, lng float64
	err := db.Conn.QueryRow(
		`SELECT target_type, target_id, lat, lng, label, created FROM locations WHERE id = ? AND site_id = ?`,
		id, db.SiteID,
	).Scan(&tt, &tid, &lat, &lng, &label, &created)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "target_type": tt, "target_id": tid,
		"lat": lat, "lng": lng, "label": label, "created": created,
	}, nil
}

// DeleteLocation removes a location by id.
func (db *DB) DeleteLocation(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM locations WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Channels & messages (community feed) ---

// ListChannels returns the site's channels (public).
func (db *DB) ListChannels() ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, name, kind, created, updated FROM channels WHERE site_id = ? ORDER BY created`,
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

// CreateChannel inserts a channel and returns its id.
func (db *DB) CreateChannel(name, kind string) (string, error) {
	if kind == "" {
		kind = "feed"
	}
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO channels (id, site_id, name, kind, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, name, kind, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetChannel returns a channel by id, or sql.ErrNoRows.
func (db *DB) GetChannel(id string) (map[string]any, error) {
	var name, kind, created, updated string
	err := db.Conn.QueryRow(
		`SELECT name, kind, created, updated FROM channels WHERE id = ? AND site_id = ?`, id, db.SiteID,
	).Scan(&name, &kind, &created, &updated)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "kind": kind, "created": created, "updated": updated}, nil
}

// DeleteChannel removes a channel and its messages.
func (db *DB) DeleteChannel(id string) error {
	res, err := db.Conn.Exec(`DELETE FROM channels WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	db.Conn.Exec(`DELETE FROM messages WHERE channel_id = ? AND site_id = ?`, id, db.SiteID)
	return nil
}

// messageSelect joins the author profile's display fields, like comments.
const messageSelect = `SELECT m.id, m.channel_id, m.parent_id, m.author_id, m.body, m.created,
       a.name, a.avatar
FROM messages m LEFT JOIN authors a ON a.id = m.author_id`

func scanMessage(scan func(...any) error) (map[string]any, error) {
	var id, channelID, parentID, authorID, body, created string
	var authorName, authorAvatar sql.NullString
	if err := scan(&id, &channelID, &parentID, &authorID, &body, &created, &authorName, &authorAvatar); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "channel_id": channelID, "parent_id": parentID, "author_id": authorID,
		"author_name": authorName.String, "author_avatar": authorAvatar.String,
		"body": body, "created": created,
	}, nil
}

// ListMessages returns a channel's messages, oldest first. Each row carries a
// `mine` flag (the viewer wrote it) for inline self-delete.
func (db *DB) ListMessages(channelID, viewerUserID string) ([]map[string]any, error) {
	q := `SELECT m.id, m.channel_id, m.parent_id, m.author_id, m.body, m.created, a.name, a.avatar,
	             CASE WHEN ? != '' AND a.user_id = ? THEN 1 ELSE 0 END AS mine
	      FROM messages m LEFT JOIN authors a ON a.id = m.author_id
	      WHERE m.site_id = ? AND m.channel_id = ? ORDER BY m.created ASC`
	rows, err := db.Conn.Query(q, viewerUserID, viewerUserID, db.SiteID, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, channelID, parentID, authorID, body, created string
		var authorName, authorAvatar sql.NullString
		var mine int
		if err := rows.Scan(&id, &channelID, &parentID, &authorID, &body, &created, &authorName, &authorAvatar, &mine); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "channel_id": channelID, "parent_id": parentID, "author_id": authorID,
			"author_name": authorName.String, "author_avatar": authorAvatar.String,
			"body": body, "created": created, "mine": mine == 1,
		})
	}
	return out, rows.Err()
}

// CreateMessage inserts a message and returns its id.
func (db *DB) CreateMessage(channelID, parentID, authorID, body string) (string, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO messages (id, site_id, channel_id, author_id, body, parent_id, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, channelID, authorID, body, parentID, now, now,
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

// --- Reactions (community) ---

// ToggleReaction adds a reaction if the author hasn't made it, or removes it if
// they have. Returns the resulting state (true = now reacted).
func (db *DB) ToggleReaction(targetType, targetID, authorID, emoji string) (bool, error) {
	var id string
	err := db.Conn.QueryRow(
		`SELECT id FROM reactions
		 WHERE site_id = ? AND target_type = ? AND target_id = ? AND author_id = ? AND emoji = ?`,
		db.SiteID, targetType, targetID, authorID, emoji,
	).Scan(&id)
	if err == nil {
		_, derr := db.Conn.Exec(`DELETE FROM reactions WHERE id = ?`, id)
		return false, derr
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, ierr := db.Conn.Exec(
		`INSERT INTO reactions (id, site_id, target_type, target_id, author_id, emoji, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		GenerateID(), db.SiteID, targetType, targetID, authorID, emoji, now,
	)
	return true, ierr
}

// ReactionCounts returns per-emoji counts for a target. When authorID is set,
// each entry's `reacted` reports whether that author made the reaction.
func (db *DB) ReactionCounts(targetType, targetID, authorID string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT emoji, COUNT(*) AS count,
		        SUM(CASE WHEN author_id = ? THEN 1 ELSE 0 END) AS mine
		 FROM reactions WHERE site_id = ? AND target_type = ? AND target_id = ?
		 GROUP BY emoji ORDER BY emoji`,
		authorID, db.SiteID, targetType, targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var emoji string
		var count, mine int
		if err := rows.Scan(&emoji, &count, &mine); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"emoji": emoji, "count": count, "reacted": mine > 0})
	}
	return out, rows.Err()
}

// --- Polls (community) ---

// Sentinel errors from CastVote so handlers can map them to status codes.
var (
	ErrPollNotFound = fmt.Errorf("poll not found")
	ErrPollClosed   = fmt.Errorf("poll closed")
	ErrAlreadyVoted = fmt.Errorf("already voted")
)

// CreatePoll inserts a poll and returns its id. options is stored as a JSON
// array; slug is optional (empty for admin-created polls, set for file-authored
// ones so they can be resolved by name).
func (db *DB) CreatePoll(postID, slug, question string, options []string, closesAt string) (string, error) {
	opts, err := json.Marshal(options)
	if err != nil {
		return "", err
	}
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.Conn.Exec(
		`INSERT INTO polls (id, site_id, post_id, slug, question, options, closes_at, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, postID, slug, question, string(opts), closesAt, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetPoll returns a poll with per-option vote tallies. When authorID is set,
// `my_vote` is the option index that author chose, or nil. Returns ErrPollNotFound
// if the poll does not exist.
func (db *DB) GetPoll(id, authorID string) (map[string]any, error) {
	var slug, question, optionsJSON, closesAt string
	err := db.Conn.QueryRow(
		`SELECT slug, question, options, closes_at FROM polls WHERE id = ? AND site_id = ?`,
		id, db.SiteID,
	).Scan(&slug, &question, &optionsJSON, &closesAt)
	if err == sql.ErrNoRows {
		return nil, ErrPollNotFound
	}
	if err != nil {
		return nil, err
	}

	var labels []string
	json.Unmarshal([]byte(optionsJSON), &labels)

	// Tally votes per option index.
	tally := map[int]int{}
	rows, err := db.Conn.Query(`SELECT option_index, COUNT(*) FROM poll_votes WHERE poll_id = ? GROUP BY option_index`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var idx, n int
		if err := rows.Scan(&idx, &n); err != nil {
			rows.Close()
			return nil, err
		}
		tally[idx] = n
	}
	rows.Close()

	total := 0
	options := []map[string]any{}
	for i, label := range labels {
		options = append(options, map[string]any{"index": i, "text": label, "votes": tally[i]})
		total += tally[i]
	}

	var myVote any
	if authorID != "" {
		var idx int
		if err := db.Conn.QueryRow(`SELECT option_index FROM poll_votes WHERE poll_id = ? AND author_id = ?`, id, authorID).Scan(&idx); err == nil {
			myVote = idx
		}
	}

	return map[string]any{
		"id":          id,
		"slug":        slug,
		"question":    question,
		"options":     options,
		"total_votes": total,
		"closes_at":   closesAt,
		"my_vote":     myVote,
	}, nil
}

// pollDef is a poll declared in a post's front matter (record.data.poll).
type pollDef struct {
	slug     string
	question string
	options  []string
	closesAt string
	postID   string
}

// findPollDef scans posts for one whose data.poll.slug matches, returning the
// declared poll definition. Empty options or question mean "no usable poll".
func (db *DB) findPollDef(slug string) *pollDef {
	rows, err := db.Conn.Query(`SELECT id, data FROM posts WHERE site_id = ?`, db.SiteID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var postID, dataJSON string
		if rows.Scan(&postID, &dataJSON) != nil {
			continue
		}
		def := parsePollDef(dataJSON)
		if def != nil && def.slug == slug {
			def.postID = postID
			return def
		}
	}
	return nil
}

// parsePollDef extracts a poll definition from a record's data JSON, or nil.
func parsePollDef(dataJSON string) *pollDef {
	var data struct {
		Poll *struct {
			Slug     string   `json:"slug"`
			Question string   `json:"question"`
			Options  []string `json:"options"`
			ClosesAt string   `json:"closes_at"`
		} `json:"poll"`
	}
	if json.Unmarshal([]byte(dataJSON), &data) != nil || data.Poll == nil {
		return nil
	}
	if data.Poll.Slug == "" || data.Poll.Question == "" || len(data.Poll.Options) < 2 {
		return nil
	}
	return &pollDef{
		slug:     data.Poll.Slug,
		question: data.Poll.Question,
		options:  data.Poll.Options,
		closesAt: data.Poll.ClosesAt,
	}
}

// ResolvePollBySlug returns the id of the poll with the given slug, creating it
// from the declaring post's front matter on first use. If the post's definition
// has since changed, the poll's question/options/closes_at are synced in place —
// existing votes are preserved. Returns ErrPollNotFound if no poll and no
// declaring post exist for the slug.
func (db *DB) ResolvePollBySlug(slug string) (string, error) {
	if slug == "" {
		return "", ErrPollNotFound
	}

	var id, postID, question, optionsJSON, closesAt string
	err := db.Conn.QueryRow(
		`SELECT id, post_id, question, options, closes_at FROM polls WHERE site_id = ? AND slug = ?`,
		db.SiteID, slug,
	).Scan(&id, &postID, &question, &optionsJSON, &closesAt)

	if err == sql.ErrNoRows {
		def := db.findPollDef(slug)
		if def == nil {
			return "", ErrPollNotFound
		}
		return db.CreatePoll(def.postID, def.slug, def.question, def.options, def.closesAt)
	}
	if err != nil {
		return "", err
	}

	// Poll exists — sync its text from the declaring post if it changed.
	def := db.findPollDef(slug)
	if def != nil {
		newOpts, _ := json.Marshal(def.options)
		if def.question != question || string(newOpts) != optionsJSON || def.closesAt != closesAt {
			db.Conn.Exec(
				`UPDATE polls SET question = ?, options = ?, closes_at = ? WHERE id = ? AND site_id = ?`,
				def.question, string(newOpts), def.closesAt, id, db.SiteID,
			)
		}
	}
	return id, nil
}

// SetRecordData replaces a post's `data` JSON blob (front-matter fields).
func (db *DB) SetRecordData(id, dataJSON string) error {
	if dataJSON == "" {
		dataJSON = "{}"
	}
	_, err := db.Conn.Exec(`UPDATE posts SET data = ? WHERE id = ? AND site_id = ?`, dataJSON, id, db.SiteID)
	return err
}

// CastVote records a member's vote. Enforces one vote per (poll, author) and
// rejects votes on a closed poll. Returns a sentinel error on each failure.
func (db *DB) CastVote(pollID, authorID string, optionIndex int) error {
	var closesAt string
	err := db.Conn.QueryRow(`SELECT closes_at FROM polls WHERE id = ? AND site_id = ?`, pollID, db.SiteID).Scan(&closesAt)
	if err == sql.ErrNoRows {
		return ErrPollNotFound
	}
	if err != nil {
		return err
	}
	if closesAt != "" {
		if t, perr := time.Parse("2006-01-02T15:04:05Z", closesAt); perr == nil && time.Now().UTC().After(t) {
			return ErrPollClosed
		}
	}

	var existing string
	if db.Conn.QueryRow(`SELECT id FROM poll_votes WHERE poll_id = ? AND author_id = ?`, pollID, authorID).Scan(&existing) == nil {
		return ErrAlreadyVoted
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.Conn.Exec(
		`INSERT INTO poll_votes (id, poll_id, option_index, author_id, created) VALUES (?, ?, ?, ?, ?)`,
		GenerateID(), pollID, optionIndex, authorID, now,
	)
	return err
}

// --- Site settings (key/value) ---

// GetSetting returns a stored setting value, or def if unset.
func (db *DB) GetSetting(key, def string) string {
	var v string
	err := db.Conn.QueryRow(`SELECT value FROM site_settings WHERE site_id = ? AND key = ?`, db.SiteID, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

// GetBoolSetting returns a stored setting as a bool ("true"/"1" are true).
func (db *DB) GetBoolSetting(key string, def bool) bool {
	v := db.GetSetting(key, "")
	switch v {
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		return def
	}
}

// AllSettings returns every stored setting for the site (for sync).
func (db *DB) AllSettings() (map[string]string, error) {
	rows, err := db.Conn.Query(`SELECT key, value FROM site_settings WHERE site_id = ?`, db.SiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSetting upserts a setting value.
func (db *DB) SetSetting(key, value string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO site_settings (site_id, key, value, updated) VALUES (?, ?, ?, ?)
		 ON CONFLICT(site_id, key) DO UPDATE SET value = excluded.value, updated = excluded.updated`,
		db.SiteID, key, value, now,
	)
	return err
}

// --- User management ---

// User represents a user account.
type User struct {
	ID           string
	SiteID       string
	Email        string
	Phone        string
	Name         string
	Avatar       string
	PasswordHash string
	Role         string
	AuthMethods  string
	Created      string
	Updated      string
}

// IsSetupDone returns true if an owner user exists for this site.
func (db *DB) IsSetupDone() bool {
	var id string
	err := db.Conn.QueryRow(
		`SELECT id FROM users WHERE site_id = ? AND role = 'owner' LIMIT 1`,
		db.SiteID,
	).Scan(&id)
	return err == nil && id != ""
}

// CountOwners returns how many owner accounts the site has — used by the
// last-owner guard, which keeps a site from ever losing its final owner.
func (db *DB) CountOwners() int {
	var n int
	db.Conn.QueryRow(`SELECT COUNT(*) FROM users WHERE site_id = ? AND role = 'owner'`, db.SiteID).Scan(&n)
	return n
}

// CreateUser creates a new user with the given details.
// The password is hashed with bcrypt before storing.
func (db *DB) CreateUser(email, name, password, role string) (*User, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	_, err = db.Conn.Exec(
		`INSERT INTO users (id, site_id, email, name, password_hash, role, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, email, name, string(hash), role, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	if _, err := db.createDefaultAuthor(id, name, email); err != nil {
		return nil, fmt.Errorf("creating default author: %w", err)
	}

	return &User{
		ID:      id,
		SiteID:  db.SiteID,
		Email:   email,
		Name:    name,
		Role:    role,
		Created: now,
		Updated: now,
	}, nil
}

// createDefaultAuthor creates a profile for an account and returns its id.
func (db *DB) createDefaultAuthor(userID, name, email string) (string, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO authors (id, site_id, user_id, name, email, role, created, updated)
		 VALUES (?, ?, ?, ?, ?, 'member', ?, ?)`,
		id, db.SiteID, userID, name, email, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListAuthors returns every author profile for the site (for sync with users).
func (db *DB) ListAuthors() ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, user_id, name, email, avatar, role, created, updated FROM authors WHERE site_id = ? ORDER BY created`,
		db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, userID, name, email, avatar, role, created, updated string
		if err := rows.Scan(&id, &userID, &name, &email, &avatar, &role, &created, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "user_id": userID, "name": name, "email": email,
			"avatar": avatar, "role": role, "created": created, "updated": updated,
		})
	}
	return out, rows.Err()
}

// UpsertAuthor inserts or updates an author profile by id (for sync).
func (db *DB) UpsertAuthor(id, userID, name, email, avatar, role, created string) error {
	if id == "" {
		return nil
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if created == "" {
		created = now
	}
	if role == "" {
		role = "member"
	}
	_, err := db.Conn.Exec(
		`INSERT INTO authors (id, site_id, user_id, name, email, avatar, role, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   user_id=excluded.user_id, name=excluded.name, email=excluded.email,
		   avatar=excluded.avatar, role=excluded.role, updated=excluded.updated`,
		id, db.SiteID, userID, name, email, avatar, role, created, now,
	)
	return err
}

// DefaultAuthorID returns the account's default persona id, or "". It honors the
// account's chosen default (users.default_author_id) when that still points at one
// of its profiles, and otherwise falls back to the earliest profile — so a member
// who has picked a persona has all their comments/messages attributed to it.
func (db *DB) DefaultAuthorID(userID string) string {
	var chosen string
	db.Conn.QueryRow(
		`SELECT default_author_id FROM users WHERE id = ? AND site_id = ?`, userID, db.SiteID,
	).Scan(&chosen)
	if chosen != "" && db.authorBelongsTo(chosen, userID) {
		return chosen
	}
	var id string
	db.Conn.QueryRow(
		`SELECT id FROM authors WHERE site_id = ? AND user_id = ? ORDER BY created LIMIT 1`,
		db.SiteID, userID,
	).Scan(&id)
	return id
}

// authorBelongsTo reports whether authorID is one of userID's profiles on this site.
func (db *DB) authorBelongsTo(authorID, userID string) bool {
	var ok string
	err := db.Conn.QueryRow(
		`SELECT id FROM authors WHERE id = ? AND site_id = ? AND user_id = ?`,
		authorID, db.SiteID, userID,
	).Scan(&ok)
	return err == nil
}

// ListPersonas returns an account's author profiles (personas), earliest first,
// each flagged with whether it's the current default (the one attribution uses).
func (db *DB) ListPersonas(userID string) ([]map[string]any, error) {
	def := db.DefaultAuthorID(userID)
	rows, err := db.Conn.Query(
		`SELECT id, name, avatar FROM authors WHERE site_id = ? AND user_id = ? ORDER BY created`,
		db.SiteID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, avatar string
		if err := rows.Scan(&id, &name, &avatar); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "avatar": avatar, "is_default": id == def})
	}
	return out, rows.Err()
}

// CreatePersona adds a new author profile (persona) to an account and returns it.
func (db *DB) CreatePersona(userID, name, avatar string) (map[string]any, error) {
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO authors (id, site_id, user_id, name, avatar, email, role, created, updated)
		 VALUES (?, ?, ?, ?, ?, '', 'member', ?, ?)`,
		id, db.SiteID, userID, name, avatar, now, now,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "avatar": avatar, "is_default": false}, nil
}

// SetDefaultPersona points an account's default at one of its own profiles. Returns
// sql.ErrNoRows if the persona doesn't belong to the account.
func (db *DB) SetDefaultPersona(userID, authorID string) error {
	if !db.authorBelongsTo(authorID, userID) {
		return sql.ErrNoRows
	}
	_, err := db.Conn.Exec(
		`UPDATE users SET default_author_id = ? WHERE id = ? AND site_id = ?`,
		authorID, userID, db.SiteID,
	)
	return err
}

// GetUserByEmail returns the account for an email, or sql.ErrNoRows.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := db.Conn.QueryRow(
		`SELECT id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated
		 FROM users WHERE site_id = ? AND email = ?`,
		db.SiteID, email,
	).Scan(&u.ID, &u.SiteID, &u.Email, &u.Phone, &u.Name, &u.Avatar, &u.PasswordHash, &u.Role, &u.AuthMethods, &u.Created, &u.Updated)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// CreateMember creates a passwordless OTP account with the given role (the
// site's access.default_role for self-serve signups) plus a default profile.
func (db *DB) CreateMember(email, name, role string) (*User, error) {
	if role == "" {
		role = "member"
	}
	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	_, err := db.Conn.Exec(
		`INSERT INTO users (id, site_id, email, name, password_hash, role, auth_methods, created, updated)
		 VALUES (?, ?, ?, ?, '', ?, '["otp"]', ?, ?)`,
		id, db.SiteID, email, name, role, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating member: %w", err)
	}
	if _, err := db.createDefaultAuthor(id, name, email); err != nil {
		return nil, fmt.Errorf("creating default author: %w", err)
	}
	return &User{ID: id, SiteID: db.SiteID, Email: email, Name: name, Role: role, Created: now, Updated: now}, nil
}

// CreateOTP stores a hashed one-time code for an account.
func (db *DB) CreateOTP(userID, code string, ttl time.Duration) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = db.Conn.Exec(
		`INSERT INTO otp_codes (id, site_id, user_id, code_hash, channel, expires_at, used, created)
		 VALUES (?, ?, ?, ?, 'email', ?, 0, ?)`,
		GenerateID(), db.SiteID, userID, string(hash),
		now.Add(ttl).Format("2006-01-02T15:04:05Z"), now.Format("2006-01-02T15:04:05Z"),
	)
	return err
}

// HasFreshOTP reports whether the account has an unused code issued within the
// given window — used to rate-limit repeated request-code calls.
func (db *DB) HasFreshOTP(userID string, within time.Duration) bool {
	cutoff := time.Now().UTC().Add(-within).Format("2006-01-02T15:04:05Z")
	var id string
	err := db.Conn.QueryRow(
		`SELECT id FROM otp_codes WHERE site_id = ? AND user_id = ? AND used = 0 AND created > ? LIMIT 1`,
		db.SiteID, userID, cutoff,
	).Scan(&id)
	return err == nil
}

// VerifyOTP checks a code against the latest unused, unexpired code for an
// account; on success it marks the code used and returns true.
func (db *DB) VerifyOTP(userID, code string) bool {
	var id, codeHash, expiresAt string
	err := db.Conn.QueryRow(
		`SELECT id, code_hash, expires_at FROM otp_codes
		 WHERE site_id = ? AND user_id = ? AND used = 0 ORDER BY created DESC LIMIT 1`,
		db.SiteID, userID,
	).Scan(&id, &codeHash, &expiresAt)
	if err != nil {
		return false
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", expiresAt); err != nil || time.Now().UTC().After(t) {
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(code)) != nil {
		return false
	}
	db.Conn.Exec(`UPDATE otp_codes SET used = 1 WHERE id = ?`, id)
	return true
}

// AuthenticateUser verifies email+password and returns the user if valid.
func (db *DB) AuthenticateUser(email, password string) (*User, error) {
	u := &User{}
	err := db.Conn.QueryRow(
		`SELECT id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated
		 FROM users WHERE site_id = ? AND email = ?`,
		db.SiteID, email,
	).Scan(&u.ID, &u.SiteID, &u.Email, &u.Phone, &u.Name, &u.Avatar, &u.PasswordHash, &u.Role, &u.AuthMethods, &u.Created, &u.Updated)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return u, nil
}

// GetUserByID returns a user by ID.
func (db *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := db.Conn.QueryRow(
		`SELECT id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated
		 FROM users WHERE id = ? AND site_id = ?`,
		id, db.SiteID,
	).Scan(&u.ID, &u.SiteID, &u.Email, &u.Phone, &u.Name, &u.Avatar, &u.PasswordHash, &u.Role, &u.AuthMethods, &u.Created, &u.Updated)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all users for this site.
func (db *DB) ListUsers() ([]*User, error) {
	rows, err := db.Conn.Query(
		`SELECT id, site_id, email, phone, name, avatar, role, auth_methods, created, updated
		 FROM users WHERE site_id = ? ORDER BY created ASC`,
		db.SiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.SiteID, &u.Email, &u.Phone, &u.Name, &u.Avatar, &u.Role, &u.AuthMethods, &u.Created, &u.Updated); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUser updates a user's name and role.
func (db *DB) UpdateUser(id, name, role string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`UPDATE users SET name = ?, role = ?, updated = ? WHERE id = ? AND site_id = ?`,
		name, role, now, id, db.SiteID,
	)
	return err
}

// UpdateUserPassword sets a new password for a user.
func (db *DB) UpdateUserPassword(id, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.Conn.Exec(
		`UPDATE users SET password_hash = ?, updated = ? WHERE id = ? AND site_id = ?`,
		string(hash), now, id, db.SiteID,
	)
	return err
}

// DeleteUser removes a user by ID. Authorization (rank guard, last-owner guard)
// is enforced by the caller.
func (db *DB) DeleteUser(id string) error {
	result, err := db.Conn.Exec(`DELETE FROM users WHERE id = ? AND site_id = ?`, id, db.SiteID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	// Clean up sessions for deleted user.
	_, _ = db.Conn.Exec(`DELETE FROM sessions WHERE user_id = ? AND site_id = ?`, id, db.SiteID)
	return nil
}

// --- Session management ---

// CreateSession creates a new session for a user and returns the token.
func (db *DB) CreateSession(userID, ipAddress, userAgent string) (string, error) {
	id := GenerateID()
	token := generateToken()
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	_, err := db.Conn.Exec(
		`INSERT INTO sessions (id, site_id, user_id, token, expires_at, ip_address, user_agent, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, db.SiteID, userID, token, expiresAt, ipAddress, userAgent, now,
	)
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}
	return token, nil
}

// ValidateSession checks if a session token is valid and returns the associated user.
func (db *DB) ValidateSession(token string) (*User, error) {
	var userID, expiresAt string
	err := db.Conn.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE site_id = ? AND token = ?`,
		db.SiteID, token,
	).Scan(&userID, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid session")
	}

	expires, err := time.Parse("2006-01-02T15:04:05Z", expiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		// Clean up expired session.
		db.Conn.Exec(`DELETE FROM sessions WHERE site_id = ? AND token = ?`, db.SiteID, token)
		return nil, fmt.Errorf("session expired")
	}

	return db.GetUserByID(userID)
}

// DeleteSession removes a session by token.
func (db *DB) DeleteSession(token string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM sessions WHERE site_id = ? AND token = ?`,
		db.SiteID, token,
	)
	return err
}

// DeleteExpiredSessions cleans up expired sessions.
func (db *DB) DeleteExpiredSessions() {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	db.Conn.Exec(`DELETE FROM sessions WHERE site_id = ? AND expires_at < ?`, db.SiteID, now)
}

// --- Migration ---

// MigrateAdminToUsers checks if the old admin table has a password and no owner
// exists yet. Returns true if migration is needed (user must provide email).
func (db *DB) MigrateAdminToUsers(email string) error {
	var hash string
	err := db.Conn.QueryRow(`SELECT password_hash FROM admin WHERE id = 'admin'`).Scan(&hash)
	if err != nil || hash == "" {
		return fmt.Errorf("no legacy admin to migrate")
	}

	// Check if an owner already exists.
	if db.IsSetupDone() {
		return fmt.Errorf("owner already exists")
	}

	id := GenerateID()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	_, err = db.Conn.Exec(
		`INSERT INTO users (id, site_id, email, name, password_hash, role, created, updated)
		 VALUES (?, ?, ?, ?, ?, 'owner', ?, ?)`,
		id, db.SiteID, email, "Admin", hash, now, now,
	)
	return err
}

// HasLegacyAdmin returns true if there's a password in the old admin table
// but no owner user yet.
func (db *DB) HasLegacyAdmin() bool {
	var hash string
	err := db.Conn.QueryRow(`SELECT password_hash FROM admin WHERE id = 'admin'`).Scan(&hash)
	return err == nil && hash != "" && !db.IsSetupDone()
}

// --- Helpers ---

func GenerateID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Rate limiting (fixed window) ---

// RateLimitExceeded reports whether a bucket has hit `limit` events within
// `window` (read-only — does not record anything).
func (db *DB) RateLimitExceeded(bucket string, limit int, window time.Duration) bool {
	var count int
	var windowStart string
	err := db.Conn.QueryRow(
		`SELECT count, window_start FROM rate_limits WHERE site_id = ? AND bucket = ?`,
		db.SiteID, bucket,
	).Scan(&count, &windowStart)
	if err != nil {
		return false
	}
	if ws, perr := time.Parse("2006-01-02T15:04:05Z", windowStart); perr == nil && time.Since(ws) > window {
		return false // window elapsed
	}
	return count >= limit
}

// RateLimitHit records one event in a bucket, starting or rolling the window.
func (db *DB) RateLimitHit(bucket string, window time.Duration) {
	now := time.Now().UTC()
	nowStr := now.Format("2006-01-02T15:04:05Z")
	var windowStart string
	err := db.Conn.QueryRow(
		`SELECT window_start FROM rate_limits WHERE site_id = ? AND bucket = ?`,
		db.SiteID, bucket,
	).Scan(&windowStart)
	if err != nil {
		db.Conn.Exec(`INSERT INTO rate_limits (site_id, bucket, count, window_start) VALUES (?, ?, 1, ?)`, db.SiteID, bucket, nowStr)
		return
	}
	if ws, perr := time.Parse("2006-01-02T15:04:05Z", windowStart); perr == nil && time.Since(ws) > window {
		db.Conn.Exec(`UPDATE rate_limits SET count = 1, window_start = ? WHERE site_id = ? AND bucket = ?`, nowStr, db.SiteID, bucket)
		return
	}
	db.Conn.Exec(`UPDATE rate_limits SET count = count + 1 WHERE site_id = ? AND bucket = ?`, db.SiteID, bucket)
}

// RateLimitAllow records an event and reports whether it stayed under `limit`.
func (db *DB) RateLimitAllow(bucket string, limit int, window time.Duration) bool {
	if db.RateLimitExceeded(bucket, limit, window) {
		return false
	}
	db.RateLimitHit(bucket, window)
	return true
}

// RateLimitClear resets a bucket (e.g. after a successful login).
func (db *DB) RateLimitClear(bucket string) {
	db.Conn.Exec(`DELETE FROM rate_limits WHERE site_id = ? AND bucket = ?`, db.SiteID, bucket)
}

// --- Roles & capabilities ---
//
// Permissions are modeled as capabilities (the atoms). A role is a named bundle
// of capabilities; the built-in bundles form a superset ladder, but enforcement
// is capability-based so presets/custom roles can deviate later. See
// design/auth-permissions.md.

type Capability string

const (
	CapContentCreate      Capability = "content.create"
	CapContentEditOwn     Capability = "content.edit.own"
	CapContentEditAny     Capability = "content.edit.any"
	CapContentPublish     Capability = "content.publish"
	CapCommentModerateOwn Capability = "comment.moderate.own"
	CapCommentModerateAny Capability = "comment.moderate.any"
	CapUserManage         Capability = "user.manage"
	CapSiteConfigure      Capability = "site.configure"
	CapSiteOwn            Capability = "site.own"
)

// roleCapabilities maps each built-in role to the capabilities it holds. Built as
// supersets: each role adds to the one below it.
var roleCapabilities = func() map[string]map[Capability]bool {
	member := map[Capability]bool{}
	contributor := merge(member, CapContentCreate, CapContentEditOwn, CapCommentModerateOwn)
	editor := merge(contributor, CapContentEditAny, CapContentPublish, CapCommentModerateAny)
	admin := merge(editor, CapUserManage, CapSiteConfigure)
	owner := merge(admin, CapSiteOwn)
	return map[string]map[Capability]bool{
		"member":      member,
		"contributor": contributor,
		"editor":      editor,
		"admin":       admin,
		"owner":       owner,
	}
}()

func merge(base map[Capability]bool, add ...Capability) map[Capability]bool {
	out := map[Capability]bool{}
	for c := range base {
		out[c] = true
	}
	for _, c := range add {
		out[c] = true
	}
	return out
}

// RoleCan reports whether a role holds a capability.
func RoleCan(role string, cap Capability) bool {
	return roleCapabilities[role][cap]
}

// Can reports whether the user's role holds a capability.
func (u *User) Can(cap Capability) bool { return RoleCan(u.Role, cap) }

// ValidRole reports whether a role name is one of the built-ins.
func ValidRole(role string) bool {
	_, ok := roleCapabilities[role]
	return ok
}

// RoleRank returns the trust level of a role (higher = more access). Used by the
// rank guard on user management; capabilities gate everything else.
func RoleRank(role string) int {
	switch role {
	case "owner":
		return 5
	case "admin":
		return 4
	case "editor":
		return 3
	case "contributor":
		return 2
	case "member":
		return 1
	default:
		return 0
	}
}

// --- Ownership ---

// UserOwnsAuthor reports whether the given author profile belongs to the account.
// (One account → many profiles; content references a profile via author_id.)
func (db *DB) UserOwnsAuthor(userID, authorID string) bool {
	if authorID == "" {
		return false
	}
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM authors WHERE site_id = ? AND user_id = ? AND id = ? LIMIT 1`,
		db.SiteID, userID, authorID,
	).Scan(&one)
	return err == nil
}

// UserOwnsPost reports whether the account authored the post (via any of its
// profiles).
func (db *DB) UserOwnsPost(userID, postID string) bool {
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM posts p JOIN authors a ON a.id = p.author_id
		 WHERE p.site_id = ? AND p.id = ? AND a.user_id = ? LIMIT 1`,
		db.SiteID, postID, userID,
	).Scan(&one)
	return err == nil
}

// UserOwnsCommentPost reports whether the comment sits on a post the account
// authored — the predicate behind comment.moderate.own.
func (db *DB) UserOwnsCommentPost(userID, commentID string) bool {
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM comments c
		 JOIN posts p ON p.id = c.post_id
		 JOIN authors a ON a.id = p.author_id
		 WHERE c.site_id = ? AND c.id = ? AND a.user_id = ? LIMIT 1`,
		db.SiteID, commentID, userID,
	).Scan(&one)
	return err == nil
}

// UserOwnsComment reports whether the account wrote the comment (via any of its
// profiles) — the predicate behind a member deleting their own comment.
func (db *DB) UserOwnsComment(userID, commentID string) bool {
	if userID == "" {
		return false
	}
	var one int
	err := db.Conn.QueryRow(
		`SELECT 1 FROM comments c JOIN authors a ON a.id = c.author_id
		 WHERE c.site_id = ? AND c.id = ? AND a.user_id = ? LIMIT 1`,
		db.SiteID, commentID, userID,
	).Scan(&one)
	return err == nil
}
