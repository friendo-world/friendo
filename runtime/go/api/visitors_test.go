package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// visitorSite mounts the API over a fresh site with the given [settings] lines.
// Its auth function mirrors admin.GetSessionUser: a session wins, but never a
// visitor's.
type visitorSite struct {
	t  *testing.T
	db *data.DB
	h  http.Handler
}

func newVisitorSite(t *testing.T, settings string) *visitorSite {
	t.Helper()
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	t.Setenv("RESEND_API_KEY", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "friendo.toml"), []byte("[site]\nname = \"t\"\n\n[settings]\n"+settings+"\n"), 0o644)
	db, err := data.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	authFunc := func(r *http.Request) *data.User {
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			if u, err := db.ValidateSession(c.Value); err == nil && !u.IsVisitor() {
				return u
			}
		}
		return nil
	}
	r := chi.NewRouter()
	r.Route("/_", func(r chi.Router) { Mount(r, db, dir, "t", authFunc, nil, nil, nil) })
	return &visitorSite{t: t, db: db, h: r}
}

// do sends a request with an optional session cookie and returns the response
// and the session cookie it set, if any.
func (s *visitorSite) do(method, path, cookie, body string) (*httptest.ResponseRecorder, string) {
	s.t.Helper()
	req := httptest.NewRequest(method, "/_/api"+path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:4444"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	set := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge > 0 {
			set = c.Value
		}
	}
	return rec, set
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
	}
	return out
}

// signIn requests and verifies a code (echoed in dev), carrying cookie along.
func (s *visitorSite) signIn(email, cookie string) (map[string]any, string) {
	s.t.Helper()
	rec, _ := s.do("POST", "/auth/request-code", cookie, `{"email":"`+email+`"}`)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("request-code = %d %s", rec.Code, rec.Body.String())
	}
	code, _ := decode(s.t, rec)["code"].(string)
	rec, set := s.do("POST", "/auth/verify-code", cookie, `{"email":"`+email+`","code":"`+code+`"}`)
	if rec.Code != http.StatusOK || set == "" {
		s.t.Fatalf("verify-code = %d %s", rec.Code, rec.Body.String())
	}
	user, _ := decode(s.t, rec)["user"].(map[string]any)
	return user, set
}

func reacted(t *testing.T, s *visitorSite, cookie, emoji string) bool {
	t.Helper()
	rec, _ := s.do("GET", "/reactions?post_id=p1", cookie, "")
	list, _ := decode(t, rec)["reactions"].([]any)
	for _, r := range list {
		m := r.(map[string]any)
		if m["emoji"] == emoji {
			return m["reacted"] == true
		}
	}
	return false
}

// With the switch off, a visitor is refused exactly as before.
func TestVisitorsOffByDefault(t *testing.T) {
	s := newVisitorSite(t, "")
	rec, set := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"👍"}`)
	if rec.Code != http.StatusUnauthorized || set != "" {
		t.Fatalf("react = %d (cookie %q), want 401 and no visitor", rec.Code, set)
	}
	rec, _ = s.do("GET", "/visitor", "", "")
	if can := decode(t, rec)["can"].(map[string]any); can["react"] != false {
		t.Fatalf("can = %v", can)
	}
}

// A visitor reacts, then signs up: same account, the reaction is still theirs.
func TestVisitorReactsThenSignsUp(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_react = true")
	rec, visitor := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"👍"}`)
	if rec.Code != http.StatusOK || visitor == "" {
		t.Fatalf("react = %d %s", rec.Code, rec.Body.String())
	}
	if !reacted(t, s, visitor, "👍") {
		t.Fatal("the visitor should see their own reaction")
	}
	if rec, _ := s.do("GET", "/me", visitor, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("/me for a visitor = %d, want 401", rec.Code)
	}
	rec, _ = s.do("GET", "/visitor", visitor, "")
	if decode(t, rec)["visitor"] == nil {
		t.Fatal("/visitor should know this browser")
	}
	// A second action reuses the visitor rather than starting another.
	if _, again := s.do("POST", "/reactions", visitor, `{"post_id":"p1","emoji":"🎉"}`); again != visitor {
		t.Fatal("the same session should be renewed")
	}

	vs, _ := s.db.ValidateSession(visitor)
	user, member := s.signIn("new@test.com", visitor)
	if user["id"] != vs.ID || user["role"] != "member" {
		t.Fatalf("sign-up should promote the visitor account: %v", user)
	}
	if !reacted(t, s, member, "👍") || !reacted(t, s, member, "🎉") {
		t.Fatal("the member should own the visitor's reactions")
	}
	if _, err := s.db.ValidateSession(visitor); err == nil {
		t.Fatal("the visitor session should be gone")
	}
}

// Signing in to an existing account merges; the account's own vote wins.
func TestVisitorMergesIntoAccount(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_react = true\nvisitors_can_vote = true")
	m, _ := s.db.CreateMember("old@test.com", "Old", "member")
	poll, _ := s.db.CreatePoll("", "fav", "Favourite?", []string{"a", "b"}, "")
	s.db.CastVote(poll, s.db.DefaultAuthorID(m.ID), 0)

	_, visitor := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"❤️"}`)
	if rec, _ := s.do("POST", "/polls/"+poll+"/vote", visitor, `{"option_index":1}`); rec.Code != http.StatusOK {
		t.Fatalf("vote = %d %s", rec.Code, rec.Body.String())
	}
	vs, _ := s.db.ValidateSession(visitor)

	user, member := s.signIn("old@test.com", visitor)
	if user["id"] != m.ID {
		t.Fatalf("signed in as %v", user["id"])
	}
	if !reacted(t, s, member, "❤️") {
		t.Fatal("the visitor's reaction should move to the account")
	}
	rec, _ := s.do("GET", "/polls/"+poll, member, "")
	p := decode(t, rec)["poll"].(map[string]any)
	if p["total_votes"] != float64(1) || p["my_vote"] != float64(0) {
		t.Fatalf("poll = %v, want the account's own vote only", p)
	}
	if _, err := s.db.GetUserByID(vs.ID); err == nil {
		t.Fatal("the visitor account should be merged away")
	}
}

// A visitor's session opens nothing a member's would.
func TestVisitorStaysOutOfMemberRoutes(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_react = true\nvisitors_can_vote = true\nvisitors_can_rsvp = true\nmembers_can_post = true")
	_, visitor := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"👍"}`)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/me", ""},
		{"GET", "/me/profiles", ""},
		{"POST", "/me/profiles", `{"name":"x"}`},
		{"GET", "/me/calendar", ""},
		{"POST", "/posts/p1/comments", `{"body":"hi"}`},
		{"POST", "/collections/posts/posts", `{"title":"x"}`},
		{"POST", "/follows", `{"profile_id":"x"}`},
		{"POST", "/chats/general/messages", `{"body":"hi"}`},
		{"GET", "/users", ""},
		{"GET", "/comments", ""},
		{"GET", "/settings", ""},
		{"GET", "/posts?status=pending", ""},
	} {
		rec, _ := s.do(c.method, c.path, visitor, c.body)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a visitor = %d, want 401/403", c.method, c.path, rec.Code)
		}
	}
}

// Asking for a code no longer makes an account; checking it does.
func TestRequestCodeMakesNoAccount(t *testing.T) {
	s := newVisitorSite(t, "")
	rec, _ := s.do("POST", "/auth/request-code", "", `{"email":"later@test.com"}`)
	code, _ := decode(t, rec)["code"].(string)
	if _, err := s.db.GetUserByEmail("later@test.com"); err == nil {
		t.Fatal("an unverified email must not have an account")
	}
	if rec, _ := s.do("POST", "/auth/verify-code", "", `{"email":"later@test.com","code":"000000"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code = %d", rec.Code)
	}
	if _, err := s.db.GetUserByEmail("later@test.com"); err == nil {
		t.Fatal("a wrong code must not make an account")
	}
	if rec, _ := s.do("POST", "/auth/verify-code", "", `{"email":"later@test.com","code":"`+code+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("right code = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.db.GetUserByEmail("later@test.com"); err != nil {
		t.Fatal("a checked code should make the account")
	}
}

// A visitor's RSVP needs a name, and the organizer sees it marked.
func TestVisitorRSVP(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_rsvp = true")
	s.db.CreateMember("sam@test.com", "Sam", "member")
	post, _ := s.db.CreateRecord("events", "club", "Club", "", "published", "")
	ev, _, _ := data.ParseWhen(map[string]any{"when": map[string]any{"start": "2026-10-06 19:00 to 20:30", "repeats": "weekly"}}, s.db.Location)
	if err := s.db.ReconcileWhen(post, ev); err != nil {
		t.Fatal(err)
	}
	path := "/posts/" + post + "/rsvps"

	rec, visitor := s.do("POST", path, "", `{"answer":"going"}`)
	if rec.Code != http.StatusBadRequest || decode(t, rec)["needs_name"] != true {
		t.Fatalf("no name = %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := s.do("POST", path, visitor, `{"answer":"going","name":"sam"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a member's name = %d, want 400", rec.Code)
	}
	rec, _ = s.do("POST", path, visitor, `{"answer":"going","name":"Robin"}`)
	if rec.Code != http.StatusOK || decode(t, rec)["mine"] != "going" {
		t.Fatalf("rsvp = %d %s", rec.Code, rec.Body.String())
	}
	// Once named, later answers need no name.
	if rec, _ := s.do("POST", path, visitor, `{"answer":"maybe"}`); rec.Code != http.StatusOK {
		t.Fatalf("second answer = %d %s", rec.Code, rec.Body.String())
	}
	list, _ := s.db.ListRSVPs(s.db.EventFor(post).ID, "")
	if len(list) != 1 || list[0]["author_name"] != "Robin (visitor)" || list[0]["visitor"] != true {
		t.Fatalf("organizer list = %v", list)
	}
	if rec, _ := s.do("DELETE", path, visitor, ""); rec.Code != http.StatusOK {
		t.Fatalf("clear = %d", rec.Code)
	}
}

// Each address may only start so many visitors.
func TestVisitorNewLimit(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_react = true")
	for i := 0; i < visitorNewLimit; i++ {
		if rec, _ := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"👍"}`); rec.Code != http.StatusOK {
			t.Fatalf("visitor %d = %d", i, rec.Code)
		}
	}
	if rec, _ := s.do("POST", "/reactions", "", `{"post_id":"p1","emoji":"👍"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over the limit = %d, want 429", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	for _, c := range []struct{ remote, cf, xff, want string }{
		{"203.0.113.9:1", "", "", "203.0.113.9"},
		{"203.0.113.9:1", "1.2.3.4", "5.6.7.8", "203.0.113.9"}, // public peer: headers are the client's own claim
		{"10.0.0.2:1", "", "6.6.6.6, 5.6.7.8", "5.6.7.8"},      // behind a proxy: the address it appended
		{"127.0.0.1:1", "1.2.3.4", "5.6.7.8", "1.2.3.4"},       // Cloudflare's header first
		{"[2001:db8:1:2:3::9]:1", "", "", "2001:db8:1:2::/64"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.cf != "" {
			r.Header.Set("CF-Connecting-IP", c.cf)
		}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, cf=%q, xff=%q) = %q, want %q", c.remote, c.cf, c.xff, got, c.want)
		}
	}
}
