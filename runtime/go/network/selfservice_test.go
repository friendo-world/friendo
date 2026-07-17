package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// TestSelfServiceDeploy covers the account-owned site flow that `friendo deploy`
// drives: an account claims a subdomain (created + owned), exchanges an in-process
// SSO session for it, and ownership is enforced against other accounts.
func TestSelfServiceDeploy(t *testing.T) {
	accounts, err := OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	defer accounts.Close()

	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	m := http.NewServeMux()
	NewAccountAuth(accounts, reg, "localhost").register(m)

	// A signed-in account (mint a session directly — device-auth is covered elsewhere).
	alice, err := accounts.EnsureAccount("alice@example.com")
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	aliceTok, err := accounts.StartSession(alice)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return rec
	}

	// 1. Claim a subdomain — created + owned by alice.
	if rec := do("POST", "/api/account/sites", aliceTok, `{"subdomain":"alice-site","name":"Alice"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create site = %d\n%s", rec.Code, rec.Body.String())
	}
	if owner, ok := accounts.SiteOwner("alice-site"); !ok || owner != alice {
		t.Fatalf("SiteOwner = %q, %v; want alice", owner, ok)
	}
	dir, ok := reg.Dir("alice-site")
	if !ok {
		t.Fatal("site folder was not provisioned")
	}

	// Re-claiming your own subdomain is idempotent (200).
	if rec := do("POST", "/api/account/sites", aliceTok, `{"subdomain":"alice-site"}`); rec.Code != http.StatusOK {
		t.Fatalf("re-claim = %d, want 200", rec.Code)
	}

	// 2. SSO exchange → a real site session for alice's owner user.
	rec := do("POST", "/api/sso/exchange", aliceTok, `{"subdomain":"alice-site"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sso exchange = %d\n%s", rec.Code, rec.Body.String())
	}
	var sso struct {
		Session string `json:"session"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &sso); sso.Session == "" {
		t.Fatalf("no session in %s", rec.Body.String())
	}
	// The session must validate in the site's own DB as the owner.
	db, err := data.Open(dir)
	if err != nil {
		t.Fatalf("open site: %v", err)
	}
	defer db.Conn.Close()
	user, err := db.ValidateSession(sso.Session)
	if err != nil {
		t.Fatalf("SSO session invalid in site: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("session user = %q, want alice@example.com", user.Email)
	}

	// 3. Ownership is enforced against another account.
	bob, _ := accounts.EnsureAccount("bob@example.com")
	bobTok, _ := accounts.StartSession(bob)
	if rec := do("POST", "/api/account/sites", bobTok, `{"subdomain":"alice-site"}`); rec.Code != http.StatusConflict {
		t.Errorf("bob claim of alice's site = %d, want 409", rec.Code)
	}
	if rec := do("POST", "/api/sso/exchange", bobTok, `{"subdomain":"alice-site"}`); rec.Code != http.StatusForbidden {
		t.Errorf("bob sso for alice's site = %d, want 403", rec.Code)
	}

	// 4. listing shows only your own sites.
	list := do("GET", "/api/account/sites", aliceTok, "")
	if !strings.Contains(list.Body.String(), "alice-site") {
		t.Errorf("alice's site list missing it: %s", list.Body.String())
	}
	if bl := do("GET", "/api/account/sites", bobTok, "").Body.String(); strings.Contains(bl, "alice-site") {
		t.Errorf("bob should not see alice's site: %s", bl)
	}
}

// TestSignupGate checks the invite-only signup policy: an unknown email can only
// create an account when signups are open (or after an operator invites it).
func TestSignupGate(t *testing.T) {
	accounts, err := OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	defer accounts.Close()

	// Default policy is invite-only.
	if got := accounts.Signups(); got != "invite" {
		t.Fatalf("default signups = %q, want invite", got)
	}

	verify := func(email string) error {
		code, err := accounts.RequestOTP(email)
		if err != nil {
			t.Fatalf("RequestOTP: %v", err)
		}
		_, err = accounts.VerifyOTP(email, code)
		return err
	}

	// Invite-only: an unknown email is rejected.
	if err := verify("stranger@example.com"); err == nil {
		t.Error("invite-only should reject an unknown email")
	}

	// Invited (pre-created) email is allowed.
	if _, err := accounts.EnsureAccount("guest@example.com"); err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	if err := verify("guest@example.com"); err != nil {
		t.Errorf("invited email should sign in: %v", err)
	}

	// Open signups: any email may create an account.
	if err := accounts.SetSignups("open"); err != nil {
		t.Fatalf("SetSignups: %v", err)
	}
	if err := verify("newcomer@example.com"); err != nil {
		t.Errorf("open signups should allow a new email: %v", err)
	}
}
