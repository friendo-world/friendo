// Covers groups (v0.6 Tier C): creating one makes you its admin (members may
// when members_can_start_groups is on), the three join rules, approval and promotion, the
// visibility rules on the page and in the API (private groups and their posts
// vanish for outsiders; a visitor gets the sign-in page), user.groups feeding
// `{% members only if … in user.groups %}`, [access] groups, the in_group filter,
// the group calendar feed, the switch, and that a record filed under a name that
// isn't a group is untouched.
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

func groupsSite(t *testing.T, toml string) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", toml)
	write("pages/index.html", "groups:{% for g in collections.groups %} {{ g.slug }}({{ g.member_count }})){% endfor %}; blog:{% for p in collections.blog %} {{ p.slug }}{% if p.group %}[{{ p.group.slug }}]{% endif %}{% endfor %}; mine:{% if user %}{{ user.groups|join:\",\" }}{% endif %}")
	write("pages/groups/[slug].html", "group {{ group.title }} {{ group.settings.visibility }}/{{ group.settings.join }} members={{ group.member_count }}{% for m in group.members %} {{ m.slug }}:{{ m.role }}{% endfor %} posts:{% for p in collections.blog|in_group:group.slug %} {{ p.slug }}{% endfor %}")
	write("pages/blog/[slug].html", "{{ post.title }}{% if post.group %} in {{ post.group.title }}{% endif %}")
	write("pages/board-room.html", "{% members only if \"board\" in user.groups %}board room for {{ user.name }}")
	write("pages/secret/index.html", "secret area")
	write("pages/login.html", "login {{ gate.required }} {{ gate.reason }} {{ gate.path }}")
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

func TestGroups(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := groupsSite(t, "[site]\nname = \"g\"\n\n[access]\ngroups = { \"/secret/*\" = \"board\" }\n")

	pat := newBrowser(t, srv.URL)
	pat.signInMember("pat@test.com")
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	visitor := newBrowser(t, srv.URL)

	// A member can't start a group until the site allows it.
	body := map[string]any{"title": "The Board", "slug": "board", "status": "published", "fields": map[string]any{"visibility": "private", "join": "request"}}
	if st, out, _ := pat.api("POST", "/collections/groups/posts", body); st != 403 {
		t.Fatalf("member creating a group with members_can_start_groups off = %d %v", st, out)
	}
	db.SetSetting(data.SettingMembersCanStartGroups, "true")
	st, out, _ := pat.api("POST", "/collections/groups/posts", body)
	if st != 201 {
		t.Fatalf("member creating a group = %d %v", st, out)
	}
	boardID := out["post"].(map[string]any)["id"].(string)
	if st, out, _ := pat.api("GET", "/groups/board", nil); st != 200 || out["group"].(map[string]any)["moderates"] != true || out["group"].(map[string]any)["mine"].(map[string]any)["role"] != "admin" {
		t.Fatalf("founder should moderate: %d %v", st, out)
	}
	st, out, _ = pat.api("POST", "/collections/groups/posts", map[string]any{"title": "Book club", "slug": "club", "status": "published"})
	if st != 201 {
		t.Fatalf("club = %d %v", st, out)
	}
	// Posts filed under each; one under a name that's no group.
	patAuthor := db.DefaultAuthorID(func() string {
		_, me, _ := pat.api("GET", "/me", nil)
		return me["user"].(map[string]any)["id"].(string)
	}())
	notes, _ := db.CreateRecord("blog", "notes", "Board notes", "…", "published", patAuthor)
	db.SetRecordData(notes, `{"group":"board"}`)
	pick, _ := db.CreateRecord("blog", "pick", "This month's pick", "…", "published", patAuthor)
	db.SetRecordData(pick, `{"group":"club"}`)
	misc, _ := db.CreateRecord("blog", "misc", "Misc", "…", "published", patAuthor)
	db.SetRecordData(misc, `{"group":"Concepts"}`)

	// Visibility: the visitor and Sam see the club, not the board or its post.
	status, page, _ := visitor.get("/")
	expect(t, "visitor index", status, 200, page, "groups: club(1)); blog: misc pick[club]; mine:")
	status, page, _ = sam.get("/")
	expect(t, "sam index", status, 200, page, "groups: club(1)); blog: misc pick[club]; mine:")
	status, page, _ = pat.get("/")
	expect(t, "pat index", status, 200, page, "groups: board(1)) club(1)); blog: misc notes[board] pick[club]; mine:board,club")
	status, page, _ = visitor.get("/groups/board")
	expect(t, "visitor on a private group", status, 401, page, "login member signin /groups/board")
	status, page, _ = sam.get("/groups/board")
	expect(t, "outsider on a private group", status, 404, page, "not found")
	status, page, _ = sam.get("/blog/notes")
	expect(t, "outsider on a private group's post", status, 404, page, "not found")
	status, page, _ = sam.get("/blog/misc")
	expect(t, "a post filed under a non-group", status, 200, page, "Misc")
	status, page, _ = pat.get("/groups/board")
	expect(t, "admin on the board", status, 200, page, "group The Board private/request members=1 pat:admin posts: notes")
	status, page, _ = visitor.get("/groups/club")
	expect(t, "visitor on a public group", status, 200, page, "group Book club public/open members=1 pat:admin posts: pick")
	if st, out, _ := sam.api("GET", "/groups", nil); st != 200 || len(out["groups"].([]any)) != 1 {
		t.Fatalf("GET /groups for sam = %d %v", st, out)
	}
	if st, _, _ := sam.api("GET", "/groups/board", nil); st != 404 {
		t.Fatalf("GET /groups/board for sam = %d, want 404", st)
	}

	// Gates: the page condition and [access] groups.
	status, page, _ = sam.get("/board-room")
	expect(t, "board room for an outsider", status, 403, page, "login member condition")
	status, page, _ = pat.get("/board-room")
	expect(t, "board room for a member", status, 200, page, "board room for pat")
	status, page, _ = visitor.get("/secret/")
	expect(t, "[access] groups signed out", status, 401, page, "login member signin")
	status, page, _ = sam.get("/secret/")
	expect(t, "[access] groups outsider", status, 403, page, "login member condition")
	status, page, _ = pat.get("/secret/")
	expect(t, "[access] groups member", status, 200, page, "secret area")

	// Joining: open joins at once; request waits; invite refuses.
	if st, out, _ := sam.api("POST", "/groups/club/join", nil); st != 200 || out["group"].(map[string]any)["mine"].(map[string]any)["status"] != "member" {
		t.Fatalf("open join = %d %v", st, out)
	}
	// Sam can't see the private board to ask; a moderator adds them.
	if st, _, _ := sam.api("POST", "/groups/board/join", nil); st != 404 {
		t.Fatalf("asking to join an invisible group = %d, want 404", st)
	}
	if st, out, _ := sam.api("POST", "/groups/"+boardID+"/members", map[string]any{"slug": "pat"}); st != 404 {
		t.Fatalf("outsider adding a member = %d %v", st, out)
	}
	// Make the board visible to members: Sam asks, Pat gets a notification and approves.
	if st, out, _ := pat.api("PUT", "/groups/board/settings", map[string]any{"visibility": "members"}); st != 200 || out["group"].(map[string]any)["settings"].(map[string]any)["visibility"] != "members" {
		t.Fatalf("settings = %d %v", st, out)
	}
	if st, out, _ := sam.api("POST", "/groups/board/join", nil); st != 200 || out["group"].(map[string]any)["mine"].(map[string]any)["status"] != "requested" {
		t.Fatalf("request join = %d %v", st, out)
	}
	if _, inbox, _ := pat.api("GET", "/me/notifications", nil); inbox["unread"].(float64) != 1 || inbox["notifications"].([]any)[0].(map[string]any)["kind"] != "join_request" {
		t.Fatalf("moderator's inbox = %v", inbox)
	}
	samAuthor := db.DefaultAuthorID(func() string {
		_, me, _ := sam.api("GET", "/me", nil)
		return me["user"].(map[string]any)["id"].(string)
	}())
	if st, out, _ := sam.api("PUT", "/groups/board/members/"+samAuthor, map[string]any{"status": "member"}); st != 403 {
		t.Fatalf("approving yourself = %d %v", st, out)
	}
	if st, out, _ := pat.api("GET", "/groups/board/members?status=requested", nil); st != 200 || len(out["members"].([]any)) != 1 {
		t.Fatalf("requested list = %d %v", st, out)
	}
	if st, out, _ := pat.api("PUT", "/groups/board/members/"+samAuthor, map[string]any{"status": "member"}); st != 200 {
		t.Fatalf("approve = %d %v", st, out)
	}
	if _, inbox, _ := sam.api("GET", "/me/notifications", nil); inbox["notifications"].([]any)[0].(map[string]any)["kind"] != "membership" {
		t.Fatalf("sam's inbox = %v", inbox)
	}
	status, page, _ = sam.get("/")
	if !strings.Contains(page, "mine:club,board") && !strings.Contains(page, "mine:board,club") {
		t.Fatalf("sam's groups = %q", page)
	}
	status, page, _ = sam.get("/board-room")
	expect(t, "board room after approval", status, 200, page, "board room for sam")
	// A moderator keeps the group tidy but doesn't run it; an admin does both.
	if st, out, _ := pat.api("PUT", "/groups/board/members/"+samAuthor, map[string]any{"role": "moderator"}); st != 200 {
		t.Fatalf("make moderator = %d %v", st, out)
	}
	if st, _, _ := sam.api("PUT", "/groups/board/settings", map[string]any{"join": "open"}); st != 403 {
		t.Fatalf("a moderator changing settings = %d, want 403", st)
	}
	if st, _, _ := sam.api("PUT", "/groups/board/members/"+samAuthor, map[string]any{"role": "admin"}); st != 403 {
		t.Fatalf("a moderator promoting = %d, want 403", st)
	}
	// Promote to admin, then the last-admin guard.
	if st, out, _ := pat.api("PUT", "/groups/board/members/"+samAuthor, map[string]any{"role": "admin"}); st != 200 {
		t.Fatalf("make admin = %d %v", st, out)
	}
	if st, _, _ := pat.api("DELETE", "/groups/board/join", nil); st != 200 {
		t.Fatalf("an admin leaves while another remains = %d", st)
	}
	if st, _, _ := sam.api("DELETE", "/groups/board/join", nil); st != 409 {
		t.Fatalf("the last admin leaving = %d, want 409", st)
	}
	// Invite only: Pat can't rejoin; Sam adds them.
	sam.api("PUT", "/groups/board/settings", map[string]any{"join": "invite"})
	if st, _, _ := pat.api("POST", "/groups/board/join", nil); st != 403 {
		t.Fatalf("joining an invite-only group = %d, want 403", st)
	}
	if st, out, _ := sam.api("POST", "/groups/board/members", map[string]any{"slug": "pat"}); st != 201 {
		t.Fatalf("add member = %d %v", st, out)
	}

	// The group feed and the public feed.
	ev, _ := db.CreateRecord("events", "agm", "AGM", "", "published", patAuthor)
	db.SetRecordData(ev, `{"group":"club"}`)
	db.ReconcileWhen(ev, mustWhen(t, "2027-03-01 19:00"))
	priv, _ := db.CreateRecord("events", "closed", "Closed session", "", "published", patAuthor)
	db.SetRecordData(priv, `{"group":"board"}`)
	db.ReconcileWhen(priv, mustWhen(t, "2027-03-02 19:00"))
	status, feed, _ := visitor.get("/calendar.json?group=club")
	if status != 200 || !strings.Contains(feed, "AGM") || strings.Contains(feed, "Closed") {
		t.Fatalf("group feed = %d %s", status, feed)
	}
	status, feed, _ = visitor.get("/calendar.json")
	if !strings.Contains(feed, "AGM") || strings.Contains(feed, "Closed") {
		t.Fatalf("public feed must hide a members group's events: %s", feed)
	}

	// Switch off: API 403 off:true, nothing hidden, no user.groups.
	db.SetSetting(data.FeatureSetting("groups"), "false")
	if st, out, _ := pat.api("GET", "/groups", nil); st != 403 || out["off"] != true {
		t.Fatalf("groups off = %d %v", st, out)
	}
	status, page, _ = sam.get("/")
	expect(t, "index while off", status, 200, page, "mine:")
	status, page, _ = sam.get("/blog/notes")
	expect(t, "nothing hidden while off", status, 200, page, "Board notes")
}

func mustWhen(t *testing.T, s string) *data.Event {
	t.Helper()
	ev, warnings, present := data.ParseWhen(map[string]any{"when": s}, nil)
	if !present || ev == nil {
		t.Fatalf("ParseWhen(%q): %v", s, warnings)
	}
	return ev
}
