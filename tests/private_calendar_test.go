// Covers private calendar feeds: a visitor's feed is public and omits gated
// collections and non-public groups; a member's (by cookie or by their token)
// carries what they may see, per-record gates checked per event, private
// caching; the token API shows, resets and refuses; ?mine=1 and ?group= narrow.
package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

func privateCalendarSite(t *testing.T) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"pc\"\n\n[access]\nmembers_only = [\"/private/*\"]\n")
	write("pages/events/[slug].html", "{{ post.title }}")
	write("pages/private/[slug].html", "{{ post.title }}")
	write("pages/notes/[slug].html", "{% members only if not post.fields.hidden %}{{ post.title }}")
	write("pages/staff/[slug].html", "{% editors only %}{{ post.title }}")
	write("pages/404.html", "not found")
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return db, srv
}

func titlesIn(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `"title":"`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestPrivateCalendarFeeds(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := privateCalendarSite(t)
	owner, _ := db.CreateMember("owner@test.com", "Owner", "owner")
	ownerA := db.DefaultAuthorID(owner.ID)
	mk := func(collection, slug, title, when, dataJSON string) string {
		id, _ := db.CreateRecord(collection, slug, title, "", "published", ownerA)
		if dataJSON != "" {
			db.SetRecordData(id, dataJSON)
		}
		db.ReconcileWhen(id, mustWhen(t, when))
		return id
	}
	fair := mk("events", "fair", "Fair", "2027-03-01 10:00", "")
	mk("private", "board-meeting", "Board meeting", "2027-03-02 18:00", "")
	mk("notes", "open-note", "Open note", "2027-03-03 18:00", "")
	mk("notes", "hidden-note", "Hidden note", "2027-03-04 18:00", `{"hidden":true}`)
	mk("staff", "staff-day", "Staff day", "2027-03-05 18:00", "")
	club := mk("events", "club-night", "Club night", "2027-03-06 19:00", `{"group":"club"}`)
	_ = club
	// A private group with an event; Pat is in it, Sam isn't.
	pat, _ := db.CreateMember("pat@test.com", "Pat", "member")
	patA := db.DefaultAuthorID(pat.ID)
	group, _ := db.CreateRecord("groups", "club", "Club", "", "published", ownerA)
	db.SetRecordData(group, `{"visibility":"private"}`)
	db.SetMembership(group, patA, data.GroupRoleMember, data.MembershipMember)

	visitor := newBrowser(t, srv.URL)
	status, body, resp := visitor.get("/calendar.json")
	if status != 200 {
		t.Fatalf("public feed = %d", status)
	}
	got := titlesIn(body)
	if !has(got, "Fair") || has(got, "Board meeting") || has(got, "Open note") || has(got, "Staff day") || has(got, "Club night") {
		t.Fatalf("visitor sees %v", got)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "public") {
		t.Fatalf("visitor Cache-Control = %q", cc)
	}
	publicETag := resp.Header.Get("ETag")

	// Pat, by cookie: the gated collection, the visible note (not the hidden one),
	// the club's event; not the editors-only one.
	patB := newBrowser(t, srv.URL)
	patB.signInMember("pat@test.com")
	status, body, resp = patB.get("/calendar.json")
	got = titlesIn(body)
	if !has(got, "Fair") || !has(got, "Board meeting") || !has(got, "Open note") || has(got, "Hidden note") || has(got, "Staff day") || !has(got, "Club night") {
		t.Fatalf("pat (cookie) sees %v", got)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "private") || !strings.Contains(resp.Header.Get("Vary"), "Cookie") {
		t.Fatalf("member headers = %q / %q", cc, resp.Header.Get("Vary"))
	}
	if resp.Header.Get("ETag") == publicETag {
		t.Fatal("a member's feed must not share the public ETag")
	}
	// Sam, not in the club: no club event.
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	_, body, _ = sam.get("/calendar.json")
	if got = titlesIn(body); has(got, "Club night") || !has(got, "Board meeting") {
		t.Fatalf("sam sees %v", got)
	}
	// An editor passes the editors-only gate.
	db.CreateMember("ed@test.com", "Ed", "editor")
	ed := newBrowser(t, srv.URL)
	ed.signInMember("ed@test.com")
	_, body, _ = ed.get("/calendar.json")
	if got = titlesIn(body); !has(got, "Staff day") {
		t.Fatalf("editor sees %v", got)
	}

	// The private link: 401 signed out; a token for Pat; the feed by token.
	if st, _, _ := visitor.api("GET", "/me/calendar", nil); st != 401 {
		t.Fatalf("GET /me/calendar signed out = %d", st)
	}
	st, out, _ := patB.api("GET", "/me/calendar", nil)
	if st != 200 {
		t.Fatalf("GET /me/calendar = %d %v", st, out)
	}
	token := out["token"].(string)
	if !strings.Contains(out["ics"].(string), "/calendar.ics?token="+token) || !strings.HasPrefix(out["webcal"].(string), "webcal://") {
		t.Fatalf("links = %v", out)
	}
	status, body, resp = visitor.get("/calendar.ics?token=" + token)
	if status != 200 || !strings.Contains(body, "Board meeting") || !strings.Contains(body, "Club night") || strings.Contains(body, "Hidden note") {
		t.Fatalf("token feed = %d %s", status, body)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Fatalf("token feed Cache-Control = %q", cc)
	}
	_, body, _ = visitor.get("/calendar.json?token=" + token)
	if !strings.Contains(body, "token="+token) {
		t.Fatal("per-event links in a token feed should carry the token")
	}
	if status, _, _ = visitor.get("/calendar.ics?token=bogus"); status != 401 {
		t.Fatalf("bogus token = %d, want 401", status)
	}
	// Reset: old dies, new works, the API shows the new one.
	st, out, _ = patB.api("POST", "/me/calendar/reset", nil)
	fresh := out["token"].(string)
	if st != 200 || fresh == token {
		t.Fatalf("reset = %d %v", st, out)
	}
	if status, _, _ = visitor.get("/calendar.ics?token=" + token); status != 401 {
		t.Fatalf("old token after reset = %d, want 401", status)
	}
	if status, _, _ = visitor.get("/calendar.ics?token=" + fresh); status != 200 {
		t.Fatalf("new token = %d", status)
	}
	if _, out, _ = patB.api("GET", "/me/calendar", nil); out["token"] != fresh {
		t.Fatal("GET /me/calendar should show the reset token")
	}

	// ?mine=1: only what Pat answered or was invited to.
	fairEv := db.EventFor(fair)
	key, _ := fairEv.ResolveOccurrence("", fairEv.Starts.AddDate(0, -1, 0))
	db.SetRSVP(fairEv.ID, key, patA, "going")
	_, body, _ = visitor.get("/calendar.json?token=" + fresh + "&mine=1")
	if got = titlesIn(body); len(got) != 1 || got[0] != "Fair" {
		t.Fatalf("mine = %v", got)
	}
	_, body, _ = visitor.get("/calendar.json?mine=1")
	if got = titlesIn(body); len(got) != 0 {
		t.Fatalf("a visitor's mine feed = %v", got)
	}
	// ?group=club: Pat yes, visitor nothing.
	_, body, _ = visitor.get("/calendar.json?token=" + fresh + "&group=club")
	if got = titlesIn(body); len(got) != 1 || got[0] != "Club night" {
		t.Fatalf("pat's club feed = %v", got)
	}
	_, body, _ = visitor.get("/calendar.json?group=club")
	if got = titlesIn(body); len(got) != 0 {
		t.Fatalf("visitor's club feed = %v", got)
	}
}
