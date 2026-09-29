// Covers follows (v0.6 Tier B): the toggle API, counts, friends, the viewer's
// user.following / user.friends in templates, record.follower_count on a profile,
// the by_following feed filter, the follows switch (403 off:true, empty context),
// and that follows ride push/pull --users.
package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

func followsSite(t *testing.T) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"f\"\n\n[settings]\nprofile_visibility = \"public\"\n")
	write("pages/feed.html", "feed:{% for p in collections.blog|by_following:user %} {{ p.title }}{% endfor %};following={{ user.following|length }} friends={{ user.friends|length }}")
	write("pages/profiles/[slug].html", "{{ profile.name }} followers={{ profile.follower_count }} following={{ profile.following_count }}{% for f in profile.followers %} <{{ f.slug }}>{% endfor %}")
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

func TestFollows(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := followsSite(t)
	pat, _ := db.CreateMember("pat@test.com", "Pat", "contributor")
	patAuthor := db.DefaultAuthorID(pat.ID)
	db.CreateRecord("blog", "hello", "Hello", "…", "published", patAuthor)
	kim, _ := db.CreateMember("kim@test.com", "Kim", "contributor")
	db.CreateRecord("blog", "other", "Other", "…", "published", db.DefaultAuthorID(kim.ID))

	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")

	// Nothing followed yet: empty feed, zero counts, not following.
	status, body, _ := sam.get("/feed")
	expect(t, "empty feed", status, 200, body, "feed:;following=0 friends=0")
	st, out, _ := sam.api("GET", "/follows?slug=pat", nil)
	if st != 200 || out["following"] != false || out["followers"].(float64) != 0 {
		t.Fatalf("GET /follows = %d %v", st, out)
	}

	// Follow Pat: the feed shows Pat's post, not Kim's; the profile page counts it.
	st, out, _ = sam.api("POST", "/follows", map[string]any{"slug": "pat"})
	if st != 200 || out["following"] != true || out["followers"].(float64) != 1 {
		t.Fatalf("POST /follows = %d %v", st, out)
	}
	status, body, _ = sam.get("/feed")
	expect(t, "feed after follow", status, 200, body, "feed: Hello;following=1 friends=0")
	status, body, _ = sam.get("/profiles/pat")
	expect(t, "pat's profile", status, 200, body, "Pat followers=1 following=0 <sam>")
	st, me, _ := sam.api("GET", "/me", nil)
	if st != 200 || len(me["user"].(map[string]any)["following"].([]any)) != 1 {
		t.Fatalf("/me.following = %v", me)
	}
	if st, out, _ := sam.api("GET", "/profiles/pat/followers", nil); st != 200 || out["followers"].(float64) != 1 {
		t.Fatalf("GET /profiles/pat/followers = %d %v", st, out)
	}

	// Pat follows Sam back → friends. Following yourself is refused.
	patB := newBrowser(t, srv.URL)
	patB.signInMember("pat@test.com")
	if st, out, _ := patB.api("POST", "/follows", map[string]any{"slug": "sam"}); st != 200 {
		t.Fatalf("pat follows sam = %d %v", st, out)
	}
	status, body, _ = sam.get("/feed")
	expect(t, "friends", status, 200, body, "friends=1")
	if st, out, _ := patB.api("POST", "/follows", map[string]any{"profile_id": patAuthor}); st != 400 {
		t.Fatalf("self follow = %d %v, want 400", st, out)
	}
	if st, _, _ := patB.api("POST", "/follows", map[string]any{"slug": "nobody"}); st != 404 {
		t.Fatalf("following nobody = %d, want 404", st)
	}
	visitor := newBrowser(t, srv.URL)
	if st, _, _ := visitor.api("POST", "/follows", map[string]any{"slug": "pat"}); st != 401 {
		t.Fatalf("signed-out follow = %d, want 401", st)
	}
	status, body, _ = visitor.get("/feed")
	expect(t, "visitor feed", status, 200, body, "feed:;following=0 friends=0")

	// Toggle again unfollows; DELETE is idempotent.
	if st, out, _ := sam.api("POST", "/follows", map[string]any{"slug": "pat"}); st != 200 || out["following"] != false {
		t.Fatalf("toggle off = %d %v", st, out)
	}
	if st, _, _ := sam.api("DELETE", "/follows/"+patAuthor, nil); st != 204 {
		t.Fatalf("DELETE /follows = %d, want 204", st)
	}
	status, body, _ = sam.get("/feed")
	expect(t, "feed after unfollow", status, 200, body, "feed:;following=0 friends=0")

	// Sync: follows ride the users pull/push.
	st, pulled, _ := patB.api("GET", "/pull/users", nil)
	if st != 403 {
		t.Fatalf("a contributor can't pull users: %d", st)
	}
	follows, _ := db.ListFollows()
	if len(follows) != 1 {
		t.Fatalf("ListFollows = %v", follows)
	}
	_ = pulled

	// The switch: off → 403 off:true, empty context, profile counts stay zero.
	db.SetSetting(data.FeatureSetting("follows"), "false")
	if st, out, _ := sam.api("GET", "/follows?slug=pat", nil); st != 403 || out["off"] != true {
		t.Fatalf("follows off = %d %v, want 403 off:true", st, out)
	}
	status, body, _ = patB.get("/feed")
	expect(t, "feed while off", status, 200, body, "feed:;following=0 friends=0")
	status, body, _ = sam.get("/profiles/sam")
	expect(t, "profile while off", status, 200, body, "sam followers=0 following=0")
}

func TestFollowsSyncWithUsers(t *testing.T) {
	src := t.TempDir()
	dbA, _ := data.Open(src)
	defer dbA.Close()
	a := newAPIClient(t, dbA, src)
	st, out := a.do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})
	a.want(201, st, out, "setup A")
	pat, _ := dbA.CreateMember("pat@test.com", "Pat", "member")
	owner := dbA.DefaultAuthorID(out["user"].(map[string]any)["id"].(string))
	if _, err := dbA.ToggleFollow(dbA.DefaultAuthorID(pat.ID), owner); err != nil {
		t.Fatal(err)
	}
	st, out = a.do("GET", "/pull/users", nil)
	a.want(200, st, out, "pull users")
	if len(out["follows"].([]any)) != 1 {
		t.Fatalf("pulled follows = %v", out["follows"])
	}

	dst := t.TempDir()
	dbB, _ := data.Open(dst)
	defer dbB.Close()
	b := newAPIClient(t, dbB, dst)
	st, setup := b.do("POST", "/setup", map[string]any{"email": "admin@b.test", "name": "B", "password": "password12345"})
	b.want(201, st, setup, "setup B")
	st, res := b.do("POST", "/push/users", map[string]any{"users": out["users"], "authors": out["authors"], "follows": out["follows"]})
	b.want(200, st, res, "push users to B")
	if res["follows"].(float64) != 1 {
		t.Fatalf("push result = %v", res)
	}
	if followers, _ := dbB.FollowCounts(owner); followers != 1 {
		t.Fatalf("owner's followers on B = %d, want 1", followers)
	}
}
