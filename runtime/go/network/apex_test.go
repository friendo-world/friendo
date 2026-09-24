package network

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApexWithoutHomeSite: a fresh network's bare domain shows the landing page
// (listing nothing), the default pages render their component, /friendo.js is
// there for them, and unknown paths are a 404.
func TestApexWithoutHomeSite(t *testing.T) {
	n := newTestNetwork(t)
	n.reg.Provision("secret", "Secret")

	rec := n.api("GET", "/", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "friendo network") {
		t.Fatalf("landing = %d\n%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the landing page must not list tenant sites")
	}
	for path, tag := range map[string]string{
		"/login": "<friendo-account>", "/account": "<friendo-account>", "/network": "<friendo-console>",
	} {
		rec := n.api("GET", path, "", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tag) || !strings.Contains(rec.Body.String(), "/friendo.js") {
			t.Errorf("%s = %d, want the %s shell:\n%s", path, rec.Code, tag, rec.Body.String())
		}
	}
	rec = n.api("GET", "/activate?code=k7qp-2xr9<b>", "", "")
	if !strings.Contains(rec.Body.String(), `<friendo-activate code="K7QP-2XR9&lt;B&gt;">`) {
		t.Errorf("activate shell should carry the escaped, upper-cased code:\n%s", rec.Body.String())
	}
	if rec := n.api("GET", "/friendo.js", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("/friendo.js at the apex = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := n.api("GET", "/nothing-here", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown apex path = %d, want 404", rec.Code)
	}
	if rec := n.api("GET", "/api/nothing", "", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "error") {
		t.Errorf("unknown api path = %d, want JSON 404", rec.Code)
	}
}

// TestApexHomeSite: with a home site set the bare domain serves it — its own
// pages, its /_/ admin, its assets — while /api/* and the default pages the
// site doesn't define stay the network's. A page the home site DOES define at
// one of those paths takes over, on purpose.
func TestApexHomeSite(t *testing.T) {
	n := newTestNetwork(t)
	tok := operatorToken(t, n.accounts, "op@x.com")
	if rec := n.api("POST", "/api/sites", tok, `{"subdomain":"www","name":"Home"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create www = %d\n%s", rec.Code, rec.Body.String())
	}
	dir, _ := n.reg.Dir("www")
	os.WriteFile(filepath.Join(dir, "pages", "index.html"), []byte("<h1>HOME SITE</h1>"), 0o644)
	os.WriteFile(filepath.Join(dir, "pages", "account.html"), []byte("<h1>BRANDED</h1><friendo-account></friendo-account>"), 0o644)
	os.MkdirAll(filepath.Join(dir, "pages", "docs"), 0o755)
	os.WriteFile(filepath.Join(dir, "pages", "docs", "[slug].html"), []byte("<h1>DOC</h1>"), 0o644)
	n.accounts.SetHomeSite("www")

	rec := n.api("GET", "/", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "HOME SITE") {
		t.Fatalf("apex with home site = %d\n%s", rec.Code, rec.Body.String())
	}
	// The home site's own page at /account wins over the network's default.
	rec = n.api("GET", "/account", "", "")
	if !strings.Contains(rec.Body.String(), "BRANDED") {
		t.Errorf("/account should be the home site's page:\n%s", rec.Body.String())
	}
	// A default page the home site doesn't define is still the network's.
	rec = n.api("GET", "/network", "", "")
	if !strings.Contains(rec.Body.String(), "<friendo-console>") {
		t.Errorf("/network should be the network's default page:\n%s", rec.Body.String())
	}
	rec = n.api("GET", "/login", "", "")
	if !strings.Contains(rec.Body.String(), "<friendo-account>") {
		t.Errorf("/login should be the network's default page:\n%s", rec.Body.String())
	}
	// /api/* is always the network's.
	if code := n.api("GET", "/api/account", "", "").Code; code != http.StatusUnauthorized {
		t.Errorf("/api/account with a home site = %d, want 401 from the network", code)
	}
	// The home site's admin shell, SDK and a 404 all come from the site.
	if rec := n.api("GET", "/_/api/setup", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "needsSetup") {
		t.Errorf("/_/api/setup at the apex should be the home site's: %d %s", rec.Code, rec.Body.String())
	}
	if rec := n.api("GET", "/friendo.js", "", ""); rec.Code != http.StatusOK {
		t.Errorf("/friendo.js at the apex = %d", rec.Code)
	}
	if rec := n.api("GET", "/no-such-page", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("home site 404 = %d", rec.Code)
	}
	// A suspended home site holds the apex.
	n.accounts.SuspendSite("www", "spam")
	if rec := n.api("GET", "/", "", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "on hold") {
		t.Errorf("suspended home site at apex = %d", rec.Code)
	}
	n.accounts.ResumeSite("www")
	// Clearing the setting goes back to the landing page.
	n.accounts.SetHomeSite("")
	if rec := n.api("GET", "/", "", ""); !strings.Contains(rec.Body.String(), "friendo network") {
		t.Errorf("apex after clearing home site should be the landing page")
	}
}

// TestWWWRedirect: www.<base> is the apex under another name, except for a www
// site's own /_/ paths, which must answer at that host.
func TestWWWRedirect(t *testing.T) {
	n := newTestNetwork(t)
	rec := n.req("GET", "www.localhost", "/about?x=1", "", "")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "http://localhost/about?x=1" {
		t.Fatalf("www redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = n.req("GET", "www.localhost:3000", "/", "", "")
	if loc := rec.Header().Get("Location"); loc != "http://localhost:3000/" {
		t.Errorf("www redirect keeps the port: %s", loc)
	}
	// No www site: even /_/ redirects.
	if rec := n.req("GET", "www.localhost", "/_/api/setup", "", ""); rec.Code != http.StatusMovedPermanently {
		t.Errorf("www /_/ without a www site = %d, want 301", rec.Code)
	}
	tok := operatorToken(t, n.accounts, "op@x.com")
	n.api("POST", "/api/sites", tok, `{"subdomain":"www"}`)
	if rec := n.req("GET", "www.localhost", "/_/api/setup", "", ""); rec.Code != http.StatusOK {
		t.Errorf("www /_/ with a www site = %d, want the site's answer", rec.Code)
	}
	if rec := n.req("GET", "www.localhost", "/", "", ""); rec.Code != http.StatusMovedPermanently {
		t.Errorf("www / with a www site = %d, want 301", rec.Code)
	}
}

// TestOpenAdminSSO: the account page's "Open admin" mints a one-time link that
// signs the owner into their site's admin at the site's own host. Only the
// owner can mint it (operators included), it works once, and only for the site
// it was minted for.
func TestOpenAdminSSO(t *testing.T) {
	n := newTestNetwork(t)
	n.accounts.SetSignups("open")
	alice := sessionFor(t, n.accounts, "alice@x.com")
	n.api("POST", "/api/account/sites", alice, `{"subdomain":"blog"}`)
	n.api("POST", "/api/account/sites", alice, `{"subdomain":"shop"}`)

	rec := n.api("POST", "/api/account/sites/blog/admin-link", "cookie:"+alice, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin-link = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"url":"http://blog.localhost/_/sso?code=`) {
		t.Fatalf("admin-link url = %s", body)
	}
	code := body[strings.Index(body, "code=")+5:]
	code = code[:strings.IndexAny(code, `"&`)]

	// The wrong site can't redeem it; then the right one can, once.
	if rec := n.req("GET", "shop.localhost", "/_/sso?code="+code, "", ""); rec.Header().Get("Location") != "/_/?error=sso" {
		t.Errorf("code redeemed at the wrong site: %s", rec.Header().Get("Location"))
	}
	rec = n.req("GET", "blog.localhost", "/_/sso?code="+code, "", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/_/" {
		t.Fatalf("sso landing = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var session string
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "friendo_session" && ck.Value != "" {
			session = ck.Value
			// Site-wide + Lax: pages render the viewer, and a link from elsewhere
			// to a members-only page still carries the session.
			if ck.Path != "/" || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode {
				t.Errorf("session cookie attributes: path=%s httponly=%v samesite=%v", ck.Path, ck.HttpOnly, ck.SameSite)
			}
		}
	}
	if session == "" {
		t.Fatal("sso landing set no friendo_session cookie")
	}
	me := httpGetWithCookie(n, "blog.localhost", "/_/api/me", session)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"owner"`) {
		t.Fatalf("me after sso = %d %s", me.Code, me.Body.String())
	}
	if rec := n.req("GET", "blog.localhost", "/_/sso?code="+code, "", ""); rec.Header().Get("Location") != "/_/?error=sso" {
		t.Error("code should be single-use")
	}
	if rec := n.req("GET", "blog.localhost", "/_/sso", "", ""); rec.Header().Get("Location") != "/_/?error=sso" {
		t.Error("missing code should bounce to the sign-in with an error")
	}

	// Not the owner: a member, and an operator who doesn't own it, both refused.
	bob := sessionFor(t, n.accounts, "bob@x.com")
	if rec := n.api("POST", "/api/account/sites/blog/admin-link", bob, ""); rec.Code != http.StatusForbidden {
		t.Errorf("bob admin-link = %d, want 403", rec.Code)
	}
	op := operatorToken(t, n.accounts, "op@x.com")
	if rec := n.api("POST", "/api/account/sites/blog/admin-link", op, ""); rec.Code != http.StatusForbidden {
		t.Errorf("operator admin-link on someone else's site = %d, want 403", rec.Code)
	}
	if rec := n.api("POST", "/api/account/sites/blog/admin-link", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous admin-link = %d, want 401", rec.Code)
	}
}

func httpGetWithCookie(n *testNetwork, host, path, session string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://"+host+path, nil)
	r.Host = host
	r.AddCookie(&http.Cookie{Name: "friendo_session", Value: session})
	rec := httptest.NewRecorder()
	n.d.ServeHTTP(rec, r)
	return rec
}
