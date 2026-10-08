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
	t   *testing.T
	db  *data.DB
	h   http.Handler
	dir string
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
	return &visitorSite{t: t, db: db, h: r, dir: dir}
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

// A visitor's RSVP needs no name — the organizer sees "Visitor" — but a name
// they give is kept and marked, and a member's name is refused.
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
	if rec.Code != http.StatusOK || decode(t, rec)["mine"] != "going" {
		t.Fatalf("anonymous rsvp = %d %s", rec.Code, rec.Body.String())
	}
	list, _ := s.db.ListRSVPs(s.db.EventFor(post).ID, "")
	if len(list) != 1 || list[0]["author_name"] != "Visitor" || list[0]["visitor"] != true {
		t.Fatalf("organizer list = %v", list)
	}
	if rec, _ := s.do("POST", path, visitor, `{"answer":"going","name":"sam"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a member's name = %d, want 400", rec.Code)
	}
	rec, _ = s.do("POST", path, visitor, `{"answer":"maybe","name":"Robin"}`)
	if rec.Code != http.StatusOK || decode(t, rec)["mine"] != "maybe" {
		t.Fatalf("named rsvp = %d %s", rec.Code, rec.Body.String())
	}
	list, _ = s.db.ListRSVPs(s.db.EventFor(post).ID, "")
	if len(list) != 1 || list[0]["author_name"] != "Robin (visitor)" {
		t.Fatalf("organizer list = %v", list)
	}
	if rec, _ := s.do("DELETE", path, visitor, ""); rec.Code != http.StatusOK {
		t.Fatalf("clear = %d", rec.Code)
	}
}

// A visitor may leave an email to be reminded the day before. It's kept on
// the answer, reported back to them and to the organizer (and nobody else),
// and refused when the server can't send email or the address isn't one.
func TestVisitorRSVPReminder(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_rsvp = true")
	post, _ := s.db.CreateRecord("events", "club", "Club", "", "published", "")
	ev, _, _ := data.ParseWhen(map[string]any{"when": map[string]any{"start": "2026-10-06 19:00 to 20:30", "repeats": "weekly"}}, s.db.Location)
	if err := s.db.ReconcileWhen(post, ev); err != nil {
		t.Fatal(err)
	}
	path := "/posts/" + post + "/rsvps"

	// No provider: the tally says so, and an email is refused.
	rec, visitor := s.do("POST", path, "", `{"answer":"going"}`)
	if rec.Code != http.StatusOK || decode(t, rec)["reminders"] != false {
		t.Fatalf("no provider: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := s.do("POST", path, visitor, `{"answer":"going","email":"me@example.com"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("email with no provider = %d, want 400", rec.Code)
	}

	t.Setenv("RESEND_API_KEY", "test")
	t.Setenv("FRIENDO_EMAIL_FROM", "Site <hi@example.com>")
	if rec, _ := s.do("POST", path, visitor, `{"answer":"going","email":"not-an-email"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad email = %d, want 400", rec.Code)
	}
	rec, _ = s.do("POST", path, visitor, `{"answer":"going","email":"Me@Example.com"}`)
	out := decode(t, rec)
	if rec.Code != http.StatusOK || out["reminders"] != true || out["reminder_email"] != "me@example.com" {
		t.Fatalf("reminder = %d %s", rec.Code, rec.Body.String())
	}
	// Answering again keeps it. The organizer's list shows it beside
	// "Visitor"; a signed-out reader of the tally never gets names at all.
	rec, _ = s.do("POST", path, visitor, `{"answer":"maybe"}`)
	if decode(t, rec)["reminder_email"] != "me@example.com" {
		t.Fatalf("a new answer dropped the reminder: %s", rec.Body.String())
	}
	list, _ := s.db.ListRSVPs(s.db.EventFor(post).ID, "")
	if len(list) != 1 || list[0]["author_name"] != "Visitor" || list[0]["author_email"] != "me@example.com" {
		t.Fatalf("organizer list = %v", list)
	}
	if _, has := out["names"]; has {
		t.Fatalf("a visitor got the names list: %v", out)
	}
	if rec, _ := s.do("GET", path, visitor, ""); rec.Code != http.StatusOK || decode(t, rec)["names"] != nil {
		t.Fatalf("a visitor's tally carries names: %s", rec.Body.String())
	}
	// The key present but empty clears it.
	rec, _ = s.do("POST", path, visitor, `{"answer":"maybe","email":""}`)
	if decode(t, rec)["reminder_email"] != "" {
		t.Fatalf("clearing = %s", rec.Body.String())
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

// A visitor's comment waits for review even when comments don't need it, shows
// only to them until approved, and is theirs to delete but not to moderate.
func TestVisitorComment(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_comment = true\ncomments_need_review = false")
	post, _ := s.db.CreateRecord("posts", "hi", "Hi", "", "published", "")
	path := "/posts/" + post + "/comments"

	rec, visitor := s.do("POST", path, "", `{"body":"hello","name":"Robin"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment = %d %s", rec.Code, rec.Body.String())
	}
	c := decode(t, rec)["comment"].(map[string]any)
	if c["status"] != "pending" || c["author_name"] != "Robin (visitor)" || c["visitor"] != true {
		t.Fatalf("comment = %v", c)
	}
	rec, _ = s.do("GET", path, visitor, "")
	mine := decode(t, rec)
	list := mine["comments"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["mine"] != true || mine["can_moderate"] != false {
		t.Fatalf("visitor's view = %v", mine)
	}
	rec, _ = s.do("GET", path, "", "")
	if list := decode(t, rec)["comments"].([]any); len(list) != 0 {
		t.Fatalf("a pending comment must not show publicly: %v", list)
	}
	if rec, _ := s.do("PUT", "/comments/"+c["id"].(string), visitor, `{"status":"approved"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a visitor approving = %d, want 401", rec.Code)
	}
	if rec, _ := s.do("DELETE", "/comments/"+c["id"].(string), visitor, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("self-delete = %d %s", rec.Code, rec.Body.String())
	}
	// A member's comment still skips review here.
	_, member := s.signIn("m@test.com", "")
	rec, _ = s.do("POST", path, member, `{"body":"hi"}`)
	if decode(t, rec)["comment"].(map[string]any)["status"] != "approved" {
		t.Fatal("members' comments should follow comments_need_review")
	}
}

// The trap field drops a bot's comment while looking like success.
func TestVisitorTrap(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_comment = true\nvisitors_can_post = true")
	post, _ := s.db.CreateRecord("posts", "hi", "Hi", "", "published", "")
	if rec, _ := s.do("POST", "/posts/"+post+"/comments", "", `{"body":"buy now","trap":"http://spam"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("trapped comment = %d", rec.Code)
	}
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"spam","trap":"x"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("trapped post = %d", rec.Code)
	}
	if list, _ := s.db.ListCommentsByStatus(""); len(list) != 0 {
		t.Fatalf("nothing should be saved: %v", list)
	}
	if list, _ := s.db.ListRecordsByStatus("pending"); len(list) != 0 {
		t.Fatalf("nothing should be saved: %v", list)
	}
}

// A visitor's post always waits for review; they can take it back until it's
// approved, can't start a group, and never moderate comments on it.
func TestVisitorPost(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_post = true\nvisitors_can_comment = true")
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"x"}`); rec.Code != http.StatusCreated {
		t.Fatalf("post = %d %s", rec.Code, rec.Body.String())
	}
	rec, visitor := s.do("POST", "/collections/tips/posts", "", `{"title":"A tip","status":"published","author_name":"Robin"}`)
	p := decode(t, rec)["post"].(map[string]any)
	if p["status"] != "pending" {
		t.Fatalf("a visitor's post must wait for review: %v", p)
	}
	id := p["id"].(string)
	if rec, _ := s.do("POST", "/collections/groups/posts", visitor, `{"title":"G"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("a visitor's group = %d, want 403", rec.Code)
	}
	// Both were made within the same second, so find Robin's rather than
	// trusting the order.
	queue, _ := s.db.ListRecordsByStatus("pending")
	var robins map[string]any
	for _, q := range queue {
		if q["id"] == id {
			robins = q
		}
	}
	if len(queue) != 2 || robins["author_name"] != "Robin (visitor)" || robins["visitor"] != true {
		t.Fatalf("review queue = %v", queue)
	}
	if rec, _ := s.do("POST", "/posts/"+id+"/withdraw", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("withdraw by nobody = %d", rec.Code)
	}
	_, other := s.do("POST", "/collections/tips/posts", "", `{"title":"z"}`)
	if rec, _ := s.do("POST", "/posts/"+id+"/withdraw", other, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("withdraw by another visitor = %d, want 404", rec.Code)
	}

	// Once approved it's the site's: no withdrawing, and still no moderating.
	s.db.SetRecordStatus(id, "published")
	if rec, _ := s.do("POST", "/posts/"+id+"/withdraw", visitor, ""); rec.Code != http.StatusConflict {
		t.Fatalf("withdraw after approval = %d, want 409", rec.Code)
	}
	_, member := s.signIn("m@test.com", "")
	crec, _ := s.do("POST", "/posts/"+id+"/comments", member, `{"body":"nice"}`)
	cid := decode(t, crec)["comment"].(map[string]any)["id"].(string)
	rec, _ = s.do("GET", "/posts/"+id+"/comments", visitor, "")
	if decode(t, rec)["can_moderate"] != false {
		t.Fatal("a visitor must not moderate comments on their own post")
	}
	if rec, _ := s.do("DELETE", "/comments/"+cid, visitor, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("a visitor deleting someone's comment = %d, want 403", rec.Code)
	}

	// A pending one can be taken back, and signing in carries the rest over.
	rec, _ = s.do("POST", "/collections/tips/posts", visitor, `{"title":"Second"}`)
	second := decode(t, rec)["post"].(map[string]any)["id"].(string)
	if rec, _ := s.do("POST", "/posts/"+second+"/withdraw", visitor, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("withdraw = %d %s", rec.Code, rec.Body.String())
	}
	user, _ := s.signIn("robin@test.com", visitor)
	if !s.db.UserOwnsPost(user["id"].(string), id) {
		t.Fatal("the post should be the new member's")
	}
}

func TestVisitorContext(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_comment = true")
	r := httptest.NewRequest("GET", "/", nil)
	ctx := VisitorContext(r, s.db, false)
	if ctx["can"].(map[string]any)["comment"] != true || ctx["known"] != false {
		t.Fatalf("visitor = %v", ctx)
	}
	if VisitorContext(r, s.db, true) != nil {
		t.Fatal("a member is not a visitor")
	}
	post, _ := s.db.CreateRecord("posts", "hi", "Hi", "", "published", "")
	_, visitor := s.do("POST", "/posts/"+post+"/comments", "", `{"body":"x","name":"Robin"}`)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: visitor})
	if ctx := VisitorContext(r, s.db, false); ctx["known"] != true || ctx["name"] != "Robin" {
		t.Fatalf("known visitor = %v", ctx)
	}
}
