package data

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

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

	// Apply schema (all statements are IF NOT EXISTS, safe to re-run).
	if _, err := conn.Exec(schemaSQL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &DB{Conn: conn, SiteID: "local"}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.Conn.Close()
}

// QueryCollection returns all posts with the given collection name as
// a slice of maps for injection into a template context.
func (db *DB) QueryCollection(collection string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT id, slug, title, body, author_id, status, published_at, created, updated
		 FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`,
		db.SiteID, collection,
	)
	if err != nil {
		return nil, fmt.Errorf("querying collection %q: %w", collection, err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, slug, title, body, authorID, status, publishedAt, created, updated string
		if err := rows.Scan(&id, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated); err != nil {
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
		})
	}
	return results, rows.Err()
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
		`SELECT id, slug, title, body, author_id, status, published_at, created, updated
		 FROM posts WHERE site_id = ? AND collection = ? AND %s = ? LIMIT 1`, fieldName,
	)
	row := db.Conn.QueryRow(query, db.SiteID, collection, value)

	var id, slug, title, body, authorID, status, publishedAt, created, updated string
	if err := row.Scan(&id, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated); err != nil {
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
