package network

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Operators is the network-level identity store: the people who run the network
// — create, list, and destroy sites — as distinct from any individual site's
// users. It lives at the network root, separate from every site database. Per
// the power boundary, an operator governs site *lifecycle and resources*, not
// the *content* inside a site.
type Operators struct {
	conn *sql.DB
}

const operatorSchema = `
CREATE TABLE IF NOT EXISTS operators (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operator_sessions (
  token       TEXT PRIMARY KEY,
  operator_id TEXT NOT NULL,
  expires_at  TEXT NOT NULL,
  created     TEXT NOT NULL
);`

// OpenOperators opens (creating if needed) the operator store under root/.network.
func OpenOperators(root string) (*Operators, error) {
	dir := filepath.Join(root, ".network")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating network state dir: %w", err)
	}
	conn, err := sql.Open("sqlite", filepath.Join(dir, "operators.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(operatorSchema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("applying operator schema: %w", err)
	}
	return &Operators{conn: conn}, nil
}

// Close releases the store.
func (o *Operators) Close() error { return o.conn.Close() }

// Count reports how many operators exist. Zero means the network needs first-run
// setup (the console shows a "create the first operator" form).
func (o *Operators) Count() (int, error) {
	var n int
	err := o.conn.QueryRow(`SELECT COUNT(*) FROM operators`).Scan(&n)
	return n, err
}

// Create adds an operator account. Errors if the email is already registered.
func (o *Operators) Create(email, password string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := o.conn.Exec(
		`INSERT INTO operators (id, email, password_hash, created) VALUES (?, ?, ?, ?)`,
		newToken(16), email, string(hash), nowRFC(),
	); err != nil {
		return fmt.Errorf("creating operator (is the email already taken?): %w", err)
	}
	return nil
}

// Authenticate verifies credentials and returns the operator id.
func (o *Operators) Authenticate(email, password string) (string, error) {
	var id, hash string
	if err := o.conn.QueryRow(`SELECT id, password_hash FROM operators WHERE email = ?`, email).Scan(&id, &hash); err != nil {
		return "", fmt.Errorf("invalid email or password")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", fmt.Errorf("invalid email or password")
	}
	return id, nil
}

// StartSession issues a 7-day session token for an operator.
func (o *Operators) StartSession(operatorID string) (string, error) {
	token := newToken(32)
	expires := time.Now().UTC().Add(7 * 24 * time.Hour).Format(rfc3339Z)
	if _, err := o.conn.Exec(
		`INSERT INTO operator_sessions (token, operator_id, expires_at, created) VALUES (?, ?, ?, ?)`,
		token, operatorID, expires, nowRFC(),
	); err != nil {
		return "", err
	}
	return token, nil
}

// ValidateSession returns the operator id for a valid, unexpired token.
func (o *Operators) ValidateSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	var operatorID, expiresAt string
	if err := o.conn.QueryRow(
		`SELECT operator_id, expires_at FROM operator_sessions WHERE token = ?`, token,
	).Scan(&operatorID, &expiresAt); err != nil {
		return "", false
	}
	if t, err := time.Parse(rfc3339Z, expiresAt); err != nil || time.Now().UTC().After(t) {
		return "", false
	}
	return operatorID, true
}

// EndSession deletes a session token (logout).
func (o *Operators) EndSession(token string) {
	o.conn.Exec(`DELETE FROM operator_sessions WHERE token = ?`, token)
}

const rfc3339Z = "2006-01-02T15:04:05Z"

func nowRFC() string { return time.Now().UTC().Format(rfc3339Z) }

// newToken returns a hex-encoded random token of nBytes of entropy.
func newToken(nBytes int) string {
	b := make([]byte, nBytes)
	rand.Read(b)
	return hex.EncodeToString(b)
}
