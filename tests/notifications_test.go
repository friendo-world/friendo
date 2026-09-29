// Covers the inbox (v0.6 Tier D): a follow and an approved comment notify the
// person they happened to (never yourself), the API lists them with an actor and a
// target, read / read-all work and only for their owner, {{ user.unread }} and
// /me.unread count them, and the inbox is off when follows and groups both are.
package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

func inboxSite(t *testing.T) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"n\"\n\n[settings]\ncomments_need_review = false\n")
	write("pages/index.html", "unread={% if user %}{{ user.unread }}{% else %}-{% endif %}")
	write("pages/blog/[slug].html", "{{ post.title }}")
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

func TestInbox(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := inboxSite(t)
	patUser, _ := db.CreateMember("pat@test.com", "Pat", "contributor")
	patAuthor := db.DefaultAuthorID(patUser.ID)
	postID, _ := db.CreateRecord("blog", "hello", "Hello", "…", "published", patAuthor)

	pat := newBrowser(t, srv.URL)
	pat.signInMember("pat@test.com")
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")

	// Nothing yet.
	st, out, _ := pat.api("GET", "/me/notifications", nil)
	if st != 200 || out["unread"].(float64) != 0 || len(out["notifications"].([]any)) != 0 {
		t.Fatalf("empty inbox = %d %v", st, out)
	}

	// Sam follows Pat and comments on Pat's post (auto-approved): two notifications.
	if st, out, _ := sam.api("POST", "/follows", map[string]any{"slug": "pat"}); st != 200 {
		t.Fatalf("follow = %d %v", st, out)
	}
	if st, out, _ := sam.api("POST", "/posts/"+postID+"/comments", map[string]any{"body": "Nice!"}); st != 201 {
		t.Fatalf("comment = %d %v", st, out)
	}
	// Pat commenting on their own post notifies nobody.
	pat.api("POST", "/posts/"+postID+"/comments", map[string]any{"body": "Thanks"})

	st, out, _ = pat.api("GET", "/me/notifications", nil)
	if st != 200 || out["unread"].(float64) != 2 {
		t.Fatalf("inbox after follow+comment = %d %v", st, out)
	}
	list := out["notifications"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 notifications, got %v", list)
	}
	newest := list[0].(map[string]any)
	if newest["kind"] != "comment" || newest["from"].(map[string]any)["slug"] != "sam" {
		t.Fatalf("newest = %v", newest)
	}
	if tgt := newest["target"].(map[string]any); tgt["title"] != "Hello" || tgt["url"] != "/blog/hello" {
		t.Fatalf("comment target = %v", tgt)
	}
	if tgt := list[1].(map[string]any)["target"].(map[string]any); tgt["url"] != "/profiles/pat" {
		t.Fatalf("follow target = %v", tgt)
	}
	status, body, _ := pat.get("/")
	expect(t, "user.unread", status, 200, body, "unread=2")
	if st, me, _ := pat.api("GET", "/me", nil); st != 200 || me["user"].(map[string]any)["unread"].(float64) != 2 {
		t.Fatalf("/me.unread = %v", me)
	}
	// Sam has nothing (and sees no badge).
	if st, out, _ := sam.api("GET", "/me/notifications", nil); st != 200 || out["unread"].(float64) != 0 {
		t.Fatalf("sam's inbox = %d %v", st, out)
	}

	// Unfollow + follow again: still one follow notification.
	sam.api("POST", "/follows", map[string]any{"slug": "pat"})
	sam.api("POST", "/follows", map[string]any{"slug": "pat"})
	if st, out, _ := pat.api("GET", "/me/notifications", nil); len(out["notifications"].([]any)) != 2 {
		t.Fatalf("dedupe: %d %v", st, out)
	}

	// Read one (only its owner can), then all.
	id := newest["id"].(string)
	if st, _, _ := sam.api("PUT", "/me/notifications/"+id+"/read", nil); st != 404 {
		t.Fatalf("sam marking pat's notification = %d, want 404", st)
	}
	if st, out, _ := pat.api("PUT", "/me/notifications/"+id+"/read", nil); st != 200 || out["unread"].(float64) != 1 {
		t.Fatalf("mark read = %d %v", st, out)
	}
	if st, out, _ := pat.api("GET", "/me/notifications?unread=1", nil); len(out["notifications"].([]any)) != 1 {
		t.Fatalf("unread=1 filter: %d %v", st, out)
	}
	if st, out, _ := pat.api("PUT", "/me/notifications/read-all", nil); st != 200 || out["unread"].(float64) != 0 {
		t.Fatalf("read-all = %d %v", st, out)
	}
	status, body, _ = pat.get("/")
	expect(t, "user.unread after read-all", status, 200, body, "unread=0")
	visitor := newBrowser(t, srv.URL)
	if st, _, _ := visitor.api("GET", "/me/notifications", nil); st != 401 {
		t.Fatalf("signed-out inbox = %d, want 401", st)
	}

	// A pending comment notifies on approval, not before.
	db.SetSetting("comments_need_review", "true")
	st, c, _ := sam.api("POST", "/posts/"+postID+"/comments", map[string]any{"body": "Later"})
	if st != 201 {
		t.Fatalf("pending comment = %d %v", st, c)
	}
	if _, out, _ := pat.api("GET", "/me/notifications", nil); out["unread"].(float64) != 0 {
		t.Fatalf("a pending comment must not notify: %v", out)
	}
	cid := c["comment"].(map[string]any)["id"].(string)
	if st, out, _ := pat.api("PUT", "/comments/"+cid, map[string]any{"status": "approved"}); st != 200 {
		t.Fatalf("approve = %d %v", st, out)
	}
	if _, out, _ := pat.api("GET", "/me/notifications", nil); out["unread"].(float64) != 1 {
		t.Fatalf("approval should notify once: %v", out)
	}

	// Both switches off → the inbox is off too.
	db.SetSetting(data.FeatureSetting("follows"), "false")
	db.SetSetting(data.FeatureSetting("groups"), "false")
	if st, out, _ := pat.api("GET", "/me/notifications", nil); st != 403 || out["off"] != true {
		t.Fatalf("inbox while off = %d %v, want 403 off:true", st, out)
	}
	status, body, _ = pat.get("/")
	expect(t, "user.unread while off", status, 200, body, "unread=0")
}
