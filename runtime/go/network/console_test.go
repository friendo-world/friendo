package network

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// do drives one request through the console, optionally form-encoded and/or
// carrying an operator session cookie.
func do(t *testing.T, c *Console, method, target string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	req := httptest.NewRequest(method, target, strings.NewReader(""))
	if form != nil {
		body = strings.NewReader(form.Encode())
		req = httptest.NewRequest(method, target, body)
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

// TestNetworkDestroyViaConsoleEvicts drives the full wired path — dispatcher +
// console + SetDestroyer(d.DestroySite) — to prove that destroying a *cached*
// site through the console stops it serving and removes its files. This is the
// integration the unit tests split apart, and the scenario the live curl walk-
// through couldn't verify cleanly (cookie/host quirk).
func TestNetworkDestroyViaConsoleEvicts(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ops, err := OpenOperators(root)
	if err != nil {
		t.Fatalf("OpenOperators: %v", err)
	}
	defer ops.Close()

	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()
	console := NewConsole(reg, ops, "localhost")
	console.SetDestroyer(d.DestroySite) // the wiring the CLI's `serve` performs
	d.HandleApex(console)

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

	ck := sessionCookie(drive("POST", "localhost", "/setup", url.Values{"email": {"op@x.com"}, "password": {"supersecret"}}, nil))
	if ck == nil {
		t.Fatal("no session cookie from setup")
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

func TestConsoleSetupAuthAndSiteLifecycle(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ops, err := OpenOperators(root)
	if err != nil {
		t.Fatalf("OpenOperators: %v", err)
	}
	defer ops.Close()
	c := NewConsole(reg, ops, "localhost")

	// Unauthenticated dashboard → redirect to /login.
	if rec := do(t, c, "GET", "http://localhost/", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth GET / = %d, want 303", rec.Code)
	}

	// With no operators, /login shows first-run setup.
	if rec := do(t, c, "GET", "http://localhost/login", nil, nil); !strings.Contains(rec.Body.String(), "Create the first operator") {
		t.Fatalf("expected setup form; got:\n%s", rec.Body.String())
	}

	// First-run setup creates the operator and signs in.
	setup := do(t, c, "POST", "http://localhost/setup", url.Values{"email": {"op@example.com"}, "password": {"supersecret"}}, nil)
	if setup.Code != http.StatusSeeOther {
		t.Fatalf("POST /setup = %d, want 303", setup.Code)
	}
	ck := sessionCookie(setup)
	if ck == nil {
		t.Fatal("setup did not set a session cookie")
	}

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

	// With an operator present, /login now shows sign-in (not setup); bad creds error.
	if rec := do(t, c, "GET", "http://localhost/login", nil, nil); !strings.Contains(rec.Body.String(), "Operator sign in") {
		t.Error("expected sign-in form once an operator exists")
	}
	if rec := do(t, c, "POST", "http://localhost/login", url.Values{"email": {"op@example.com"}, "password": {"wrong"}}, nil); !strings.Contains(strings.ToLower(rec.Body.String()), "invalid") {
		t.Errorf("bad login did not surface an error; body:\n%s", rec.Body.String())
	}
}
