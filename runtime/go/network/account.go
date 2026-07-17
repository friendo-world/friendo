package network

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Accounts is the network-level identity store for the two-tier model: everyday
// users who own sites and operators (an account with the "operator" capability).
// Login is passwordless — email + a one-time code — and the CLI links via the
// device-auth flow. See design/network-accounts.md.
//
// This is the generalized successor to Operators; Operators stays for the current
// password console until that path is retired (design Phase 3).
type Accounts struct {
	conn *sql.DB
}

const accountSchema = `
CREATE TABLE IF NOT EXISTS accounts (
  id           TEXT PRIMARY KEY,
  email        TEXT NOT NULL UNIQUE,
  capabilities TEXT NOT NULL DEFAULT '',
  created      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS account_sessions (
  token      TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS account_otp (
  email      TEXT NOT NULL,
  code_hash  TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS device_codes (
  device_code TEXT PRIMARY KEY,
  user_code   TEXT NOT NULL UNIQUE,
  account_id  TEXT,
  approved    INTEGER NOT NULL DEFAULT 0,
  expires_at  TEXT NOT NULL,
  created     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS site_owners (
  subdomain  TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  created    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS network_settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`

// Account is a network account.
type Account struct {
	ID           string
	Email        string
	Capabilities []string
}

// Has reports whether the account holds a capability (e.g. "operator").
func (a *Account) Has(cap string) bool {
	for _, c := range a.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// OpenAccounts opens (creating if needed) the accounts store under root/.network.
func OpenAccounts(root string) (*Accounts, error) {
	dir := filepath.Join(root, ".network")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating network state dir: %w", err)
	}
	conn, err := sql.Open("sqlite", filepath.Join(dir, "accounts.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(accountSchema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("applying accounts schema: %w", err)
	}
	return &Accounts{conn: conn}, nil
}

func (a *Accounts) Close() error { return a.conn.Close() }

// EnsureAccount returns the id of the account for email, creating it if absent.
func (a *Accounts) EnsureAccount(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", fmt.Errorf("email is required")
	}
	var id string
	err := a.conn.QueryRow(`SELECT id FROM accounts WHERE email = ?`, email).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = newToken(16)
	if _, err := a.conn.Exec(
		`INSERT INTO accounts (id, email, capabilities, created) VALUES (?, ?, '', ?)`,
		id, email, nowRFC(),
	); err != nil {
		return "", fmt.Errorf("creating account: %w", err)
	}
	return id, nil
}

// Grant adds a capability to an account (creating the account if needed).
func (a *Accounts) Grant(email, capability string) error {
	id, err := a.EnsureAccount(email)
	if err != nil {
		return err
	}
	acct, ok := a.Get(id)
	if !ok {
		return fmt.Errorf("account vanished")
	}
	if acct.Has(capability) {
		return nil
	}
	caps := append(acct.Capabilities, capability)
	_, err = a.conn.Exec(`UPDATE accounts SET capabilities = ? WHERE id = ?`, strings.Join(caps, ","), id)
	return err
}

// Get returns an account by id.
func (a *Accounts) Get(id string) (*Account, bool) {
	return a.scanAccount(`SELECT id, email, capabilities FROM accounts WHERE id = ?`, id)
}

// GetByEmail returns an account by email.
func (a *Accounts) GetByEmail(email string) (*Account, bool) {
	return a.scanAccount(`SELECT id, email, capabilities FROM accounts WHERE email = ?`, strings.ToLower(strings.TrimSpace(email)))
}

func (a *Accounts) scanAccount(query, arg string) (*Account, bool) {
	var id, email, caps string
	if err := a.conn.QueryRow(query, arg).Scan(&id, &email, &caps); err != nil {
		return nil, false
	}
	acct := &Account{ID: id, Email: email}
	for _, c := range strings.Split(caps, ",") {
		if c = strings.TrimSpace(c); c != "" {
			acct.Capabilities = append(acct.Capabilities, c)
		}
	}
	return acct, true
}

// Count returns how many accounts exist.
func (a *Accounts) Count() (int, error) {
	var n int
	err := a.conn.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n, err
}

// --- passwordless OTP (for in-browser account auth) ---

// RequestOTP creates a fresh 6-digit login code for email (10-minute TTL) and
// returns it. The caller emails it (or echoes it in dev). An account is not
// created until the code is verified.
func (a *Accounts) RequestOTP(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", fmt.Errorf("email is required")
	}
	code, err := numericCode(6)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	expires := time.Now().UTC().Add(10 * time.Minute).Format(rfc3339Z)
	if _, err := a.conn.Exec(
		`INSERT INTO account_otp (email, code_hash, expires_at, created) VALUES (?, ?, ?, ?)`,
		email, string(hash), expires, nowRFC(),
	); err != nil {
		return "", err
	}
	return code, nil
}

// VerifyOTP checks a login code for email; on success it ensures the account
// exists and returns its id. Codes are single-use.
func (a *Accounts) VerifyOTP(email, code string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	// Read all candidate hashes first, then close the cursor before writing —
	// SQLite dislikes a write on the same connection while a read cursor is open.
	rows, err := a.conn.Query(
		`SELECT code_hash, expires_at FROM account_otp WHERE email = ? ORDER BY created DESC`, email,
	)
	if err != nil {
		return "", err
	}
	type cand struct{ hash, expiresAt string }
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.hash, &c.expiresAt); err == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()

	now := time.Now().UTC()
	for _, c := range cands {
		if t, err := time.Parse(rfc3339Z, c.expiresAt); err != nil || now.After(t) {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(code)) == nil {
			// Signup gate: an unknown email can only create an account when signups
			// are open (invite mode = an operator must pre-create/invite the account).
			if _, exists := a.GetByEmail(email); !exists && a.Signups() == "invite" {
				return "", fmt.Errorf("sign-ups are invite-only on this network — ask an operator to invite you")
			}
			a.conn.Exec(`DELETE FROM account_otp WHERE email = ?`, email) // codes are single-use
			return a.EnsureAccount(email)
		}
	}
	return "", fmt.Errorf("invalid or expired code")
}

// --- account sessions (Bearer tokens the CLI caches) ---

// StartSession issues a 30-day account session token.
func (a *Accounts) StartSession(accountID string) (string, error) {
	token := newToken(32)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Format(rfc3339Z)
	if _, err := a.conn.Exec(
		`INSERT INTO account_sessions (token, account_id, expires_at, created) VALUES (?, ?, ?, ?)`,
		token, accountID, expires, nowRFC(),
	); err != nil {
		return "", err
	}
	return token, nil
}

// ValidateSession returns the account for a valid, unexpired token.
func (a *Accounts) ValidateSession(token string) (*Account, bool) {
	if token == "" {
		return nil, false
	}
	var accountID, expiresAt string
	if err := a.conn.QueryRow(
		`SELECT account_id, expires_at FROM account_sessions WHERE token = ?`, token,
	).Scan(&accountID, &expiresAt); err != nil {
		return nil, false
	}
	if t, err := time.Parse(rfc3339Z, expiresAt); err != nil || time.Now().UTC().After(t) {
		return nil, false
	}
	return a.Get(accountID)
}

// --- device-auth (OAuth-device-style) ---

// StartDevice creates a pending device authorization and returns the (secret)
// device code the CLI polls with and the short user code the human approves.
func (a *Accounts) StartDevice() (deviceCode, userCode string, err error) {
	deviceCode = newToken(32)
	userCode, err = humanCode()
	if err != nil {
		return "", "", err
	}
	expires := time.Now().UTC().Add(15 * time.Minute).Format(rfc3339Z)
	if _, err := a.conn.Exec(
		`INSERT INTO device_codes (device_code, user_code, approved, expires_at, created) VALUES (?, ?, 0, ?, ?)`,
		deviceCode, userCode, expires, nowRFC(),
	); err != nil {
		return "", "", err
	}
	return deviceCode, userCode, nil
}

// ApproveDevice binds a user code to an account (called from the /activate page
// after the human authenticates).
func (a *Accounts) ApproveDevice(userCode, accountID string) error {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	res, err := a.conn.Exec(
		`UPDATE device_codes SET account_id = ?, approved = 1 WHERE user_code = ? AND approved = 0
		 AND expires_at > ?`,
		accountID, userCode, nowRFC(),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown or expired code")
	}
	return nil
}

// PollDevice reports the status of a device authorization. Once approved it
// issues an account session token (once) and consumes the device code.
func (a *Accounts) PollDevice(deviceCode string) (token string, pending bool, err error) {
	var accountID sql.NullString
	var approved int
	var expiresAt string
	if err := a.conn.QueryRow(
		`SELECT account_id, approved, expires_at FROM device_codes WHERE device_code = ?`, deviceCode,
	).Scan(&accountID, &approved, &expiresAt); err != nil {
		return "", false, fmt.Errorf("unknown device code")
	}
	if t, err := time.Parse(rfc3339Z, expiresAt); err != nil || time.Now().UTC().After(t) {
		a.conn.Exec(`DELETE FROM device_codes WHERE device_code = ?`, deviceCode)
		return "", false, fmt.Errorf("device code expired")
	}
	if approved == 0 || !accountID.Valid {
		return "", true, nil // still pending
	}
	tok, err := a.StartSession(accountID.String)
	if err != nil {
		return "", false, err
	}
	a.conn.Exec(`DELETE FROM device_codes WHERE device_code = ?`, deviceCode)
	return tok, false, nil
}

// --- site ownership ---

// SetSiteOwner records (or reassigns) the owning account of a subdomain.
func (a *Accounts) SetSiteOwner(subdomain, accountID string) error {
	_, err := a.conn.Exec(
		`INSERT INTO site_owners (subdomain, account_id, created) VALUES (?, ?, ?)
		 ON CONFLICT(subdomain) DO UPDATE SET account_id = excluded.account_id`,
		subdomain, accountID, nowRFC(),
	)
	return err
}

// SiteOwner returns the account id that owns a subdomain, if any.
func (a *Accounts) SiteOwner(subdomain string) (string, bool) {
	var id string
	if err := a.conn.QueryRow(`SELECT account_id FROM site_owners WHERE subdomain = ?`, subdomain).Scan(&id); err != nil {
		return "", false
	}
	return id, true
}

// SitesOwnedBy returns the subdomains an account owns.
func (a *Accounts) SitesOwnedBy(accountID string) ([]string, error) {
	rows, err := a.conn.Query(`SELECT subdomain FROM site_owners WHERE account_id = ? ORDER BY subdomain`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			subs = append(subs, s)
		}
	}
	return subs, nil
}

// RemoveSiteOwner drops the ownership record for a subdomain (on destroy).
func (a *Accounts) RemoveSiteOwner(subdomain string) {
	a.conn.Exec(`DELETE FROM site_owners WHERE subdomain = ?`, subdomain)
}

// --- network settings ---

// Signups reports the signup policy ("open" or "invite"); defaults to "invite".
func (a *Accounts) Signups() string {
	var v string
	if err := a.conn.QueryRow(`SELECT value FROM network_settings WHERE key = 'signups'`).Scan(&v); err != nil || v == "" {
		return "invite"
	}
	return v
}

// SetSignups sets the signup policy.
func (a *Accounts) SetSignups(v string) error {
	if v != "open" && v != "invite" {
		return fmt.Errorf("signups must be 'open' or 'invite'")
	}
	_, err := a.conn.Exec(
		`INSERT INTO network_settings (key, value) VALUES ('signups', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, v,
	)
	return err
}

// numericCode returns an n-digit numeric string.
func numericCode(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String(), nil
}

// humanCode returns a short, unambiguous, uppercase code like "K7QP-2XR9".
func humanCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I, O, 0, 1
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String(), nil
}
