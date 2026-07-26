package network

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// mustAccounts opens an accounts store for a console test, cleaned up on exit.
func mustAccounts(t *testing.T, root string) *Accounts {
	t.Helper()
	a, err := OpenAccounts(root)
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// sessionFor mints an account session token for email (no capabilities).
func sessionFor(t *testing.T, accounts *Accounts, email string) string {
	t.Helper()
	id, err := accounts.EnsureAccount(email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	tok, err := accounts.StartSession(id)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return tok
}

// operatorToken mints a session token for email after granting it operator.
func operatorToken(t *testing.T, accounts *Accounts, email string) string {
	t.Helper()
	if err := accounts.Grant(email, "operator"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	acct, _ := accounts.GetByEmail(email)
	tok, err := accounts.StartSession(acct.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return tok
}

// operatorCookieFor returns an operator session cookie for email.
func operatorCookieFor(t *testing.T, accounts *Accounts, email string) *http.Cookie {
	return &http.Cookie{Name: operatorCookie, Value: operatorToken(t, accounts, email)}
}

// TestOperatorAPIAuth exercises the CLI-facing operator API: account Bearer token
// gated on the operator capability, and create/list/destroy of sites — driven
// through the dispatcher so the destroy path also evicts the live handler.
func TestOperatorAPIAuth(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	console := NewConsole(reg, accounts, "localhost")
	console.SetDestroyer(d.DestroySite)
	d.HandleApex(console)

	jreq := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Host = "localhost"
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		d.ServeHTTP(rec, req)
		return rec
	}

	// Unauthenticated → 401.
	if code := jreq("GET", "/api/sites", "", "").Code; code != http.StatusUnauthorized {
		t.Fatalf("unauth /api/sites = %d, want 401", code)
	}
	// A signed-in but non-operator account → 403.
	if code := jreq("GET", "/api/sites", sessionFor(t, accounts, "member@x.com"), "").Code; code != http.StatusForbidden {
		t.Fatalf("non-operator /api/sites = %d, want 403", code)
	}

	tok := operatorToken(t, accounts, "op@x.com")
	if code := jreq("GET", "/api/whoami", tok, "").Code; code != http.StatusOK {
		t.Errorf("whoami with operator token = %d, want 200", code)
	}
	// Create a site via the API.
	if code := jreq("POST", "/api/sites", tok, `{"subdomain":"alpha","name":"Alpha"}`).Code; code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	if _, ok := reg.Dir("alpha"); !ok {
		t.Fatal("alpha not provisioned via API")
	}
	if body := jreq("GET", "/api/sites", tok, "").Body.String(); !strings.Contains(body, "alpha") {
		t.Errorf("list missing alpha: %s", body)
	}
	// Cache the site, then destroy via API → evicted and gone.
	if code := get(t, d, "alpha.localhost", "/").Code; code != http.StatusOK {
		t.Fatalf("serve alpha = %d, want 200", code)
	}
	if code := jreq("DELETE", "/api/sites/alpha", tok, "").Code; code != http.StatusNoContent {
		t.Fatalf("destroy = %d, want 204", code)
	}
	if _, ok := reg.Dir("alpha"); ok {
		t.Error("alpha not removed via API")
	}
	if code := get(t, d, "alpha.localhost", "/").Code; code != http.StatusNotFound {
		t.Errorf("alpha after API destroy = %d, want 404", code)
	}
}

// do drives one request through the console, optionally form-encoded and/or
// carrying an operator session cookie.
func do(t *testing.T, c *Console, method, target string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(""))
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	c.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == operatorCookie && ck.Value != "" {
			return ck
		}
	}
	return nil
}

// TestConsoleOTPLogin covers the passwordless operator sign-in: the first verified
// email claims operator (bootstrap), and once operators exist a non-operator
// account is refused.
func TestConsoleOTPLogin(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	c := NewConsole(reg, accounts, "localhost")

	// The sign-in page asks for an email (passwordless).
	if body := do(t, c, "GET", "http://localhost/login", nil, nil).Body.String(); !strings.Contains(body, "Operator sign in") {
		t.Fatalf("login page not shown:\n%s", body)
	}

	// First-run bootstrap: no operators yet, so the first verified email claims it.
	// (Drive step 2 directly with a code from RequestOTP; step 1 just emails it.)
	code, err := accounts.RequestOTP("boss@x.com")
	if err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
	rec := do(t, c, "POST", "http://localhost/login", url.Values{"email": {"boss@x.com"}, "code": {code}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bootstrap login = %d, want 303; body:\n%s", rec.Code, rec.Body.String())
	}
	ck := sessionCookie(rec)
	if ck == nil {
		t.Fatal("bootstrap login set no session cookie")
	}
	if acct, ok := accounts.GetByEmail("boss@x.com"); !ok || !acct.Has("operator") {
		t.Error("first sign-in did not claim operator")
	}
	// The session works on the dashboard.
	if code := do(t, c, "GET", "http://localhost/", nil, ck).Code; code != http.StatusOK {
		t.Errorf("authed dashboard = %d, want 200", code)
	}

	// Now that an operator exists, an existing non-operator account is refused.
	if _, err := accounts.EnsureAccount("member@x.com"); err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	code2, _ := accounts.RequestOTP("member@x.com")
	rec2 := do(t, c, "POST", "http://localhost/login", url.Values{"email": {"member@x.com"}, "code": {code2}}, nil)
	if sessionCookie(rec2) != nil {
		t.Error("a non-operator account should not get a session")
	}
	if !strings.Contains(rec2.Body.String(), "an operator of this network") {
		t.Errorf("expected 'not an operator' message; got:\n%s", rec2.Body.String())
	}
}

// TestConsoleAuthAndSiteLifecycle covers auth gating + create/list/destroy through
// the browser console with an operator session cookie.
func TestConsoleAuthAndSiteLifecycle(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	c := NewConsole(reg, accounts, "localhost")

	// Unauthenticated dashboard → redirect to /login.
	if rec := do(t, c, "GET", "http://localhost/", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth GET / = %d, want 303", rec.Code)
	}

	ck := operatorCookieFor(t, accounts, "op@example.com")

	// Authenticated dashboard renders.
	if rec := do(t, c, "GET", "http://localhost/", nil, ck); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "site(s)") {
		t.Fatalf("authed dashboard = %d, body:\n%s", rec.Code, rec.Body.String())
	}

	// Create a site via the console.
	if rec := do(t, c, "POST", "http://localhost/sites", url.Values{"subdomain": {"alice"}, "name": {"Alice"}}, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("authed POST /sites = %d, want 303", rec.Code)
	}
	if _, ok := reg.Dir("alice"); !ok {
		t.Fatal("site alice was not provisioned")
	}
	if rec := do(t, c, "GET", "http://localhost/", nil, ck); !strings.Contains(rec.Body.String(), "alice.localhost") {
		t.Error("dashboard does not list the new site")
	}

	// Unauthenticated create is rejected (redirect) and provisions nothing.
	if rec := do(t, c, "POST", "http://localhost/sites", url.Values{"subdomain": {"bob"}}, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("unauth POST /sites = %d, want 303 redirect", rec.Code)
	}
	if _, ok := reg.Dir("bob"); ok {
		t.Error("unauthenticated request provisioned a site")
	}

	// Destroy a site via the console.
	if rec := do(t, c, "POST", "http://localhost/sites/alice/destroy", url.Values{}, ck); rec.Code != http.StatusSeeOther {
		t.Errorf("authed destroy = %d, want 303", rec.Code)
	}
	if _, ok := reg.Dir("alice"); ok {
		t.Error("site alice was not destroyed")
	}
}

// TestNetworkDestroyViaConsoleEvicts drives the full wired path — dispatcher +
// console + SetDestroyer(d.DestroySite) — to prove that destroying a *cached* site
// through the console stops it serving and removes its files.
func TestNetworkDestroyViaConsoleEvicts(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	console := NewConsole(reg, accounts, "localhost")
	console.SetDestroyer(d.DestroySite) // the wiring the CLI's `serve` performs
	d.HandleApex(console)

	ck := operatorCookieFor(t, accounts, "op@x.com")
	drive := func(method, host, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Host = host
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		d.ServeHTTP(rec, req) // everything goes through the dispatcher
		return rec
	}

	drive("POST", "localhost", "/sites", url.Values{"subdomain": {"zeta"}}, ck)

	// Serve once to cache the handler + open the DB.
	if code := drive("GET", "zeta.localhost", "/", url.Values{}, nil).Code; code != http.StatusOK {
		t.Fatalf("zeta pre-destroy = %d, want 200", code)
	}
	// Destroy through the console (which routes to the dispatcher).
	if code := drive("POST", "localhost", "/sites/zeta/destroy", url.Values{}, ck).Code; code != http.StatusSeeOther {
		t.Fatalf("console destroy = %d, want 303", code)
	}
	if _, ok := reg.Dir("zeta"); ok {
		t.Error("folder still present after console destroy")
	}
	if code := drive("GET", "zeta.localhost", "/", url.Values{}, nil).Code; code != http.StatusNotFound {
		t.Errorf("zeta after console destroy = %d, want 404", code)
	}
}

// TestConsoleSignupsAndInvite drives the operator UI for signup policy + invites.
func TestConsoleSignupsAndInvite(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	accounts := mustAccounts(t, root)
	c := NewConsole(reg, accounts, "localhost")
	ck := operatorCookieFor(t, accounts, "op@example.com")

	// Default policy is invite-only, and the dashboard says so.
	if body := do(t, c, "GET", "http://localhost/", nil, ck).Body.String(); !strings.Contains(body, "Invite-only") {
		t.Errorf("dashboard should show invite-only by default:\n%s", body)
	}

	// Switch to open.
	if rec := do(t, c, "POST", "http://localhost/signups", url.Values{"policy": {"open"}}, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /signups = %d, want 303", rec.Code)
	}
	if got := accounts.Signups(); got != "open" {
		t.Errorf("signups = %q, want open", got)
	}

	// Invite someone as an operator.
	if rec := do(t, c, "POST", "http://localhost/invite",
		url.Values{"email": {"friend@example.com"}, "operator": {"1"}}, ck); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /invite = %d, want 303", rec.Code)
	}
	acct, ok := accounts.GetByEmail("friend@example.com")
	if !ok {
		t.Fatal("invite did not create the account")
	}
	if !acct.Has("operator") {
		t.Error("invited account should have the operator capability")
	}

	// Dashboard now lists the invited person and shows the open policy.
	if body := do(t, c, "GET", "http://localhost/", nil, ck).Body.String(); !strings.Contains(body, "friend@example.com") || !strings.Contains(body, "Open") {
		t.Errorf("dashboard missing invited account or open policy:\n%s", body)
	}

	// Unauthenticated policy change is rejected and changes nothing.
	if rec := do(t, c, "POST", "http://localhost/signups", url.Values{"policy": {"invite"}}, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("unauth POST /signups = %d, want 303 redirect", rec.Code)
	}
	if got := accounts.Signups(); got != "open" {
		t.Error("unauthenticated request changed the signup policy")
	}
}
