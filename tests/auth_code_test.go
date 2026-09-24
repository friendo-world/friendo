// Covers v0.5 sign-in: a site signs in with an emailed code by default, passwords
// are opt-in (access.password_login), first-run setup creates no account until
// the code verifies, and code guesses are rate-limited like password attempts.
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

// apiClient wraps a cookie-jar HTTP client over the mounted /_/api.
type apiClient struct {
	t    *testing.T
	base string
	c    *http.Client
}

func newAPIClient(t *testing.T, db *data.DB, siteDir string) *apiClient {
	t.Helper()
	r := chi.NewRouter()
	admin.Mount(r, db, false, "testsite", siteDir, nil, nil, nil)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &apiClient{t: t, base: srv.URL + "/_/api", c: &http.Client{Jar: jar}}
}

func (a *apiClient) do(method, path string, body any) (int, map[string]any) {
	a.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.base+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.c.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (a *apiClient) want(status int, gotStatus int, got map[string]any, label string) {
	a.t.Helper()
	if gotStatus != status {
		a.t.Fatalf("%s: status %d, want %d (%v)", label, gotStatus, status, got)
	}
}

func TestCodeSignInIsTheDefault(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	siteDir := t.TempDir()
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	a := newAPIClient(t, db, siteDir)

	// A fresh site: needs setup, no passwords, no email provider.
	st, out := a.do("GET", "/setup", nil)
	a.want(200, st, out, "setup status")
	if out["needsSetup"] != true || out["passwordLogin"] != false || out["emailConfigured"] != false {
		t.Fatalf("setup status = %v", out)
	}

	// Setup without a code or password is refused with guidance.
	st, out = a.do("POST", "/setup", map[string]any{"email": "Owner@Test.com", "name": "Owner"})
	a.want(400, st, out, "setup with nothing")

	// Request a setup code: still no account until it verifies.
	st, out = a.do("POST", "/setup/request-code", map[string]any{"email": "Owner@Test.com"})
	a.want(200, st, out, "setup request-code")
	code, _ := out["code"].(string)
	if code == "" {
		t.Fatalf("dev echo should return the code, got %v", out)
	}
	if db.IsSetupDone() {
		t.Fatal("no owner should exist before the code verifies")
	}
	if _, err := db.GetUserByEmail("owner@test.com"); err == nil {
		t.Fatal("no user row should exist before the code verifies")
	}

	// Resend is throttled.
	st, out = a.do("POST", "/setup/request-code", map[string]any{"email": "owner@test.com"})
	a.want(429, st, out, "setup request-code resend")

	// Wrong code → 401; right code → owner created, signed in, email lowercased.
	st, out = a.do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "code": "000000"})
	if code == "000000" {
		t.Skip("astronomically unlucky code")
	}
	a.want(401, st, out, "setup wrong code")
	st, out = a.do("POST", "/setup", map[string]any{"email": "OWNER@test.com", "name": "Owner", "code": code})
	a.want(201, st, out, "setup with code")
	user := out["user"].(map[string]any)
	if user["email"] != "owner@test.com" || user["role"] != "owner" {
		t.Fatalf("owner = %v", user)
	}
	st, out = a.do("GET", "/me", nil)
	a.want(200, st, out, "me after setup")

	// The owner is a code-only account; password sign-in is off for the site.
	st, out = a.do("POST", "/auth/login", map[string]any{"email": "owner@test.com", "password": "whatever1"})
	a.want(403, st, out, "password login while off")

	// Users can be created without a password …
	st, out = a.do("POST", "/users", map[string]any{"email": "Ed@Test.com", "name": "Ed", "role": "editor"})
	a.want(201, st, out, "create user without password")
	ed, err := db.GetUserByEmail("ed@test.com")
	if err != nil {
		t.Fatalf("editor missing: %v", err)
	}
	if ed.PasswordHash != "" || ed.AuthMethods != `["otp"]` {
		t.Fatalf("editor should be code-only: hash=%q methods=%s", ed.PasswordHash, ed.AuthMethods)
	}
	// … but a short one is still rejected.
	st, out = a.do("POST", "/users", map[string]any{"email": "x@test.com", "name": "X", "role": "member", "password": "short"})
	a.want(400, st, out, "short password")

	// Turn passwords on; the editor still has none, so login fails until one is set.
	st, out = a.do("PUT", "/settings", map[string]any{"access": map[string]any{"password_login": true}})
	a.want(200, st, out, "enable password login")
	if out["access"].(map[string]any)["password_login"] != true {
		t.Fatalf("settings payload didn't report password_login on: %v", out)
	}
	st, out = a.do("PUT", "/users/"+ed.ID, map[string]any{"name": "Ed", "role": "editor", "password": "password12345"})
	a.want(200, st, out, "set editor password")

	// Sign out, then sign back in as the editor with the password (mixed case).
	a.do("POST", "/auth/logout", nil)
	st, out = a.do("POST", "/auth/login", map[string]any{"email": "ED@test.com", "password": "password12345"})
	a.want(200, st, out, "editor password login")
	st, _ = a.do("GET", "/setup", nil)
	// And the code path works for any role, with wrong guesses capped at 5.
	a.do("POST", "/auth/logout", nil)
	st, out = a.do("POST", "/auth/request-code", map[string]any{"email": "owner@test.com"})
	a.want(200, st, out, "request-code owner")
	ownerCode, _ := out["code"].(string)
	for i := 0; i < 5; i++ {
		st, out = a.do("POST", "/auth/verify-code", map[string]any{"email": "owner@test.com", "code": "999999"})
		if ownerCode == "999999" {
			t.Skip("astronomically unlucky code")
		}
		a.want(401, st, out, "wrong code")
	}
	st, out = a.do("POST", "/auth/verify-code", map[string]any{"email": "owner@test.com", "code": ownerCode})
	a.want(429, st, out, "locked out after 5 wrong codes")
}

func TestSetupWithPasswordOptsIn(t *testing.T) {
	siteDir := t.TempDir()
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	a := newAPIClient(t, db, siteDir)

	// The pre-0.5 shape (what an older CLI sends) still creates the owner — and
	// choosing a password at setup is the opt-in, so password login turns on.
	st, out := a.do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})
	a.want(201, st, out, "setup with password")
	if !db.GetBoolSetting("access.password_login", false) {
		t.Fatal("setting up with a password should turn password sign-in on")
	}
	a.do("POST", "/auth/logout", nil)
	st, out = a.do("POST", "/auth/login", map[string]any{"email": "owner@test.com", "password": "password12345"})
	a.want(200, st, out, "password login after password setup")

	// The owner can switch passwords off again; the code path keeps working.
	st, out = a.do("PUT", "/settings", map[string]any{"access": map[string]any{"password_login": false}})
	a.want(200, st, out, "disable password login")
	a.do("POST", "/auth/logout", nil)
	st, out = a.do("POST", "/auth/login", map[string]any{"email": "owner@test.com", "password": "password12345"})
	a.want(403, st, out, "password login after disabling")
}

func TestMigrateNeedsTheLegacyPassword(t *testing.T) {
	siteDir := t.TempDir()
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	// Plant a legacy single-admin password.
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldsecret"), bcrypt.MinCost)
	if _, err := db.Conn.Exec(`INSERT INTO admin (id, password_hash) VALUES ('admin', ?)`, string(hash)); err != nil {
		t.Fatalf("seeding legacy admin: %v", err)
	}
	a := newAPIClient(t, db, siteDir)

	st, out := a.do("GET", "/setup", nil)
	a.want(200, st, out, "setup status")
	if out["hasLegacyAdmin"] != true {
		t.Fatalf("expected hasLegacyAdmin, got %v", out)
	}
	// No password, or the wrong one → refused; the site stays unclaimed.
	st, out = a.do("POST", "/migrate", map[string]any{"email": "owner@test.com"})
	a.want(401, st, out, "migrate without password")
	st, out = a.do("POST", "/migrate", map[string]any{"email": "owner@test.com", "password": "nope"})
	a.want(401, st, out, "migrate wrong password")
	if db.IsSetupDone() {
		t.Fatal("migrate must not create an owner without the legacy password")
	}
	// The right one upgrades it to an owner who can keep using that password.
	st, out = a.do("POST", "/migrate", map[string]any{"email": "Owner@Test.com", "password": "oldsecret"})
	a.want(200, st, out, "migrate with legacy password")
	if !db.IsSetupDone() {
		t.Fatal("migrate should have created the owner")
	}
	st, out = a.do("POST", "/auth/login", map[string]any{"email": "owner@test.com", "password": "oldsecret"})
	a.want(200, st, out, "login with the migrated password")
}
