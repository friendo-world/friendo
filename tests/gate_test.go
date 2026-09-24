// Covers members-only pages: the session cookie reaches page renders, templates
// see {{ user }}, {% members only %} / {% editors only %} / `if` conditions gate a
// page (from the page, a layout, or the direct-file fallback), and a blocked
// visitor gets the site's login page — or the built-in one — at the same URL.
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

// gateSite writes a small site with public, gated and login pages and serves it
// through the real site handler (admin + API + pages).
func gateSite(t *testing.T) (siteDir string, srv *httptest.Server) {
	t.Helper()
	siteDir = t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("layouts/base.html", "<html><body>{% block content %}{% endblock %}</body></html>")
	write("layouts/gated.html", "{% members only %}<html><body>{% block content %}{% endblock %}</body></html>")
	write("pages/index.html", "{% extends \"layouts/base.html\" %}{% block content %}home{% if user %} hi {{ user.name }} ({{ user.role }}){% endif %}{% endblock %}")
	write("pages/members.html", "{% extends \"layouts/base.html\" %}\n{% members only %}\n{% block content %}members area for {{ user.name }}{% endblock %}")
	write("pages/staff.html", "{% editors only %}staff area")
	write("pages/vip.html", "{% members only if user.email == \"vip@test.com\" %}vip area")
	write("pages/inlayout.html", "{% extends \"layouts/gated.html\" %}{% block content %}in a gated layout{% endblock %}")
	write("pages/login.html", "login {{ gate.required }} {{ gate.reason }} {{ gate.path }}")
	write("pages/404.html", "not found on {{ site.name }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatalf("BuildSiteHandler: %v", err)
	}
	srv = httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return siteDir, srv
}

// browser is a cookie-jar client: pages and API calls share the session, as in a
// real browser.
type browser struct {
	t    *testing.T
	base string
	c    *http.Client
}

func newBrowser(t *testing.T, base string) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, base: base, c: &http.Client{Jar: jar}}
}

func (b *browser) get(path string) (int, string, *http.Response) {
	b.t.Helper()
	resp, err := b.c.Get(b.base + path)
	if err != nil {
		b.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp
}

func (b *browser) api(method, path string, payload any) (int, map[string]any, *http.Response) {
	b.t.Helper()
	var rdr io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		rdr = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.base+"/_/api"+path, rdr)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp
}

// signInMember creates (or signs in) a code-only member for email and returns the
// verify-code response, whose Set-Cookie the tests inspect.
func (b *browser) signInMember(email string) *http.Response {
	b.t.Helper()
	status, out, _ := b.api("POST", "/auth/request-code", map[string]any{"email": email})
	if status != 200 {
		b.t.Fatalf("request-code for %s: %d %v", email, status, out)
	}
	code, _ := out["code"].(string)
	if code == "" {
		b.t.Fatalf("no echoed code for %s (FRIENDO_OTP_ECHO not honoured?)", email)
	}
	status, out, resp := b.api("POST", "/auth/verify-code", map[string]any{"email": email, "code": code})
	if status != 200 {
		b.t.Fatalf("verify-code for %s: %d %v", email, status, out)
	}
	return resp
}

func expect(t *testing.T, label string, gotStatus, wantStatus int, body, wantContains string) {
	t.Helper()
	if gotStatus != wantStatus {
		t.Fatalf("%s: status %d, want %d\nbody: %s", label, gotStatus, wantStatus, body)
	}
	if wantContains != "" && !strings.Contains(body, wantContains) {
		t.Fatalf("%s: body %q does not contain %q", label, body, wantContains)
	}
}

func TestMembersOnlyPages(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	siteDir, srv := gateSite(t)

	// A visitor: public pages render with no user; gated ones show login.html in
	// place with the gate context, whether the tag is in the page, a layout, or
	// reached through the trailing-slash fallback. A 404 stays a 404.
	visitor := newBrowser(t, srv.URL)
	status, body, resp := visitor.get("/")
	expect(t, "visitor home", status, 200, body, "home")
	if strings.Contains(body, "hi ") {
		t.Fatalf("visitor saw a user: %q", body)
	}
	if resp.Header.Get("Vary") != "Cookie" {
		t.Errorf("pages vary by viewer; Vary = %q", resp.Header.Get("Vary"))
	}
	status, body, resp = visitor.get("/members")
	expect(t, "visitor /members", status, 401, body, "login member signin /members")
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("blocked page Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	status, body, _ = visitor.get("/staff")
	expect(t, "visitor /staff", status, 401, body, "login editor signin /staff")
	status, body, _ = visitor.get("/inlayout")
	expect(t, "visitor gated layout", status, 401, body, "login member signin /inlayout")
	status, body, _ = visitor.get("/members/")
	expect(t, "visitor trailing slash", status, 401, body, "login member signin")
	status, body, _ = visitor.get("/nothing")
	expect(t, "visitor 404", status, 404, body, "not found on")

	// The owner (first-run setup with a password) sees everything, and the home
	// page greets them — the session cookie now reaches page renders.
	owner := newBrowser(t, srv.URL)
	status, out, _ := owner.api("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})
	if status != 201 {
		t.Fatalf("setup: %d %v", status, out)
	}
	status, body, _ = owner.get("/")
	expect(t, "owner home", status, 200, body, "hi Owner (owner)")
	status, body, _ = owner.get("/members")
	expect(t, "owner /members", status, 200, body, "members area for Owner")
	status, body, _ = owner.get("/staff")
	expect(t, "owner /staff", status, 200, body, "staff area")
	status, body, _ = owner.get("/inlayout")
	expect(t, "owner gated layout", status, 200, body, "in a gated layout")
	status, body, _ = owner.get("/vip")
	expect(t, "owner /vip (condition false)", status, 403, body, "login member condition /vip")

	// A member (emailed code) gets members-only pages, not editors-only ones.
	member := newBrowser(t, srv.URL)
	verify := member.signInMember("m@test.com")
	var sessionCookie *http.Cookie
	for _, ck := range verify.Cookies() {
		if ck.Name == "friendo_session" && ck.Value != "" {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil {
		t.Fatal("verify-code set no session cookie")
	}
	if sessionCookie.Path != "/" || sessionCookie.SameSite != http.SameSiteLaxMode || !sessionCookie.HttpOnly {
		t.Errorf("session cookie: path=%q samesite=%v httponly=%v; want site-wide Lax HttpOnly", sessionCookie.Path, sessionCookie.SameSite, sessionCookie.HttpOnly)
	}
	status, body, _ = member.get("/members")
	expect(t, "member /members", status, 200, body, "members area for")
	status, body, _ = member.get("/staff")
	expect(t, "member /staff", status, 403, body, "login editor role /staff")
	status, body, _ = member.get("/vip")
	expect(t, "member /vip", status, 403, body, "login member condition /vip")

	// The `if` condition passes for the right account.
	vip := newBrowser(t, srv.URL)
	vip.signInMember("vip@test.com")
	status, body, _ = vip.get("/vip")
	expect(t, "vip /vip", status, 200, body, "vip area")

	// Logout clears the cookie on both its current path and the legacy /_/ path.
	status, _, resp = member.api("POST", "/auth/logout", nil)
	if status != 200 && status != 204 {
		t.Fatalf("logout: %d", status)
	}
	cleared := map[string]bool{}
	for _, ck := range resp.Cookies() {
		if ck.Name == "friendo_session" && ck.MaxAge < 0 {
			cleared[ck.Path] = true
		}
	}
	if !cleared["/"] || !cleared["/_/"] {
		t.Errorf("logout cleared paths %v; want both / and /_/", cleared)
	}
	status, body, _ = member.get("/members")
	expect(t, "member after logout", status, 401, body, "login member signin")

	// With no login.html, the built-in sign-in page stands in — with
	// <friendo-auth reload> so the page comes back once you're signed in.
	os.Remove(filepath.Join(siteDir, "pages", "login.html"))
	status, body, _ = visitor.get("/members")
	expect(t, "built-in login page", status, 401, body, "<friendo-auth reload>")
	if !strings.Contains(body, "Sign in to see this page") {
		t.Errorf("built-in page message missing: %q", body)
	}
	status, body = member.signInMemberAndGet("m@test.com", "/staff")
	expect(t, "built-in page for the wrong role", status, 403, body, "editors and up")
}

// signInMemberAndGet signs in and fetches a page in one step.
func (b *browser) signInMemberAndGet(email, path string) (int, string) {
	b.t.Helper()
	b.signInMember(email)
	status, body, _ := b.get(path)
	return status, body
}

// A broken gate tag is a template error for that page, not a silent public page.
func TestBrokenGateTagIsATemplateError(t *testing.T) {
	siteDir, srv := gateSite(t)
	os.WriteFile(filepath.Join(siteDir, "pages", "broken.html"), []byte("{% members only if %}secret"), 0o644)
	visitor := newBrowser(t, srv.URL)
	status, body, _ := visitor.get("/broken")
	if status != 500 || strings.Contains(body, "secret") {
		t.Fatalf("broken gate: %d %q; want 500 without the content", status, body)
	}
}
