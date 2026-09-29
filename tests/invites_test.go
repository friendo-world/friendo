// Covers event invitations (v0.6 Tier E): the organizer asks people by address,
// group and followers; each gets an "invited" RSVP and an inbox note; anyone
// else is refused; the invited member sees mine = invited, answers, and the tally
// moves; the CSV lists the ones who haven't answered.
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

func invitesSite(t *testing.T) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"i\"\n\n[settings]\nmembers_can_start_groups = true\n")
	write("pages/events/[slug].html", "{{ post.title }} going={{ post.rsvps.going }} invited={{ post.rsvps.invited }}")
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

func TestEventInvitations(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := invitesSite(t)
	patUser, _ := db.CreateMember("pat@test.com", "Pat", "contributor")
	patAuthor := db.DefaultAuthorID(patUser.ID)
	post, _ := db.CreateRecord("events", "party", "Party", "", "published", patAuthor)
	db.ReconcileWhen(post, mustWhen(t, "2027-06-01 19:00"))

	pat := newBrowser(t, srv.URL)
	pat.signInMember("pat@test.com")
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	kim := newBrowser(t, srv.URL)
	kim.signInMember("kim@test.com")
	lee := newBrowser(t, srv.URL)
	lee.signInMember("lee@test.com")

	// A group Kim is in, and Lee follows Pat.
	st, out, _ := kim.api("POST", "/collections/groups/posts", map[string]any{"title": "Board", "slug": "board", "status": "published"})
	if st != 201 {
		t.Fatalf("group = %d %v", st, out)
	}
	if st, _, _ := lee.api("POST", "/follows", map[string]any{"slug": "pat"}); st != 200 {
		t.Fatalf("lee follows pat = %d", st)
	}

	// Only the organizer (or a moderator) may invite.
	if st, out, _ := sam.api("POST", "/posts/"+post+"/invites", map[string]any{"slugs": []string{"kim"}}); st != 403 {
		t.Fatalf("non-organizer inviting = %d %v", st, out)
	}
	// Pat invites Sam by address, the board (Kim), and followers (Lee).
	st, out, _ = pat.api("POST", "/posts/"+post+"/invites", map[string]any{"slugs": []string{"sam", "ghost"}, "group": "board", "followers": true})
	if st != 200 || out["invited"].(float64) != 3 || out["skipped"].(float64) != 0 || out["unknown"].([]any)[0] != "ghost" {
		t.Fatalf("invite = %d %v", st, out)
	}
	if out["counts"].(map[string]any)["invited"].(float64) != 3 {
		t.Fatalf("counts = %v", out["counts"])
	}
	// Each invitee has a note in the inbox and sees mine = invited.
	for _, b := range []*browser{sam, kim, lee} {
		if _, inbox, _ := b.api("GET", "/me/notifications", nil); inbox["unread"].(float64) < 1 || inbox["notifications"].([]any)[0].(map[string]any)["kind"] != "event_invite" {
			t.Fatalf("invitee inbox = %v", inbox)
		}
		if _, r, _ := b.api("GET", "/posts/"+post+"/rsvps", nil); r["mine"] != "invited" {
			t.Fatalf("mine = %v", r)
		}
	}
	tgt := func() map[string]any {
		_, inbox, _ := sam.api("GET", "/me/notifications", nil)
		return inbox["notifications"].([]any)[0].(map[string]any)["target"].(map[string]any)
	}()
	if tgt["title"] != "Party" || tgt["url"] != "/events/party" {
		t.Fatalf("invite target = %v", tgt)
	}
	status, page, _ := sam.get("/events/party")
	expect(t, "ssr tally", status, 200, page, "Party going=0 invited=3")

	// Sam answers: the invitation becomes the answer; a second invite skips them.
	if st, r, _ := sam.api("POST", "/posts/"+post+"/rsvps", map[string]any{"answer": "going"}); st != 200 || r["mine"] != "going" {
		t.Fatalf("answer = %d %v", st, r)
	}
	status, page, _ = sam.get("/events/party")
	expect(t, "ssr tally after answer", status, 200, page, "Party going=1 invited=2")
	if st, out, _ := pat.api("POST", "/posts/"+post+"/invites", map[string]any{"slugs": []string{"sam"}}); st != 200 || out["skipped"].(float64) != 1 || out["invited"].(float64) != 0 {
		t.Fatalf("re-invite = %d %v", st, out)
	}
	// A member can't give themselves the invited answer.
	if st, _, _ := kim.api("POST", "/posts/"+post+"/rsvps", map[string]any{"answer": "invited"}); st != 400 {
		t.Fatalf("self-invite answer = %d, want 400", st)
	}
	// Attendees + CSV show who hasn't answered.
	if st, out, _ := pat.api("GET", "/posts/"+post+"/rsvps/names", nil); st != 200 || len(out["names"].([]any)) != 3 {
		t.Fatalf("rsvp names = %d %v", st, out)
	}
	status, csvBody, _ := pat.get("/_/api/posts/" + post + "/rsvps/names?format=csv")
	if status != 200 || strings.Count(csvBody, ",invited,") != 2 || !strings.Contains(csvBody, ",going,") {
		t.Fatalf("csv = %d %s", status, csvBody)
	}
	// With rsvp off, inviting is off too.
	db.SetSetting(data.FeatureSetting("rsvp"), "false")
	if st, out, _ := pat.api("POST", "/posts/"+post+"/invites", map[string]any{"slugs": []string{"kim"}}); st != 403 || out["off"] != true {
		t.Fatalf("invites while rsvp off = %d %v", st, out)
	}
}
