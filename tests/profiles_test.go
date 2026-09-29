// Covers member profiles (v0.6 Tier A): pages/profiles/[slug].html reads the
// authors table, its visibility follows the profile_visibility setting (members
// by default → the sign-in page in place; public → everyone), every record
// carries record.author, collections.profiles and the by_author filter work, and
// the profiles API answers the same way.
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

func profileSite(t *testing.T, toml string) (*data.DB, *httptest.Server) {
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
	write("pages/index.html", "people:{% for p in collections.profiles %} {{ p.slug }}{% endfor %};")
	write("pages/profiles/[slug].html", "profile {{ profile.name }} [{{ profile.slug }}] bio={{ profile.bio }} posts={{ profile.posts|length }}{% for p in profile.posts %} {{ p.title }}{% endfor %}")
	write("pages/blog/[slug].html", "{{ post.title }} by {{ post.author.name }} ({{ post.author.url }})")
	write("pages/by.html", "{% for p in collections.blog|by_author:\"pat\" %}{{ p.title }};{% endfor %}")
	write("pages/login.html", "login {{ gate.required }} {{ gate.reason }} {{ gate.path }}")
	write("pages/404.html", "not found")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatalf("BuildSiteHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return db, srv
}

func TestProfilePagesFollowVisibility(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := profileSite(t, "[site]\nname = \"p\"\n")

	// Pat is a contributor with a published post; Sam is a member.
	pat, err := db.CreateMember("pat@test.com", "Pat", "contributor")
	if err != nil {
		t.Fatal(err)
	}
	patAuthor := db.DefaultAuthorID(pat.ID)
	if _, err := db.CreateRecord("blog", "hello", "Hello", "…", "published", patAuthor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateProfile(pat.ID, patAuthor, data.ProfileUpdate{Bio: strPtr("Gardener")}); err != nil {
		t.Fatal(err)
	}

	visitor := newBrowser(t, srv.URL)

	// Members-only (the default): a visitor gets the sign-in page in place, 401.
	status, body, _ := visitor.get("/profiles/pat")
	expect(t, "profile signed out", status, 401, body, "login member signin /profiles/pat")
	status, body, _ = visitor.get("/")
	expect(t, "collections.profiles signed out", status, 200, body, "people:;")
	// record.author is on every post regardless.
	status, body, _ = visitor.get("/blog/hello")
	expect(t, "post.author", status, 200, body, "Hello by Pat (/profiles/pat)")
	status, body, _ = visitor.get("/by")
	expect(t, "by_author filter", status, 200, body, "Hello;")
	if st, out, _ := visitor.api("GET", "/profiles/pat", nil); st != 401 {
		t.Fatalf("GET /profiles/pat signed out = %d %v, want 401", st, out)
	}

	// A signed-in member sees it.
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	status, body, _ = sam.get("/profiles/pat")
	expect(t, "profile as member", status, 200, body, "profile Pat [pat] bio=Gardener posts=1 Hello")
	status, body, _ = sam.get("/")
	if !strings.Contains(body, " pat") || !strings.Contains(body, " sam") {
		t.Fatalf("collections.profiles as member = %q", body)
	}
	if st, out, _ := sam.api("GET", "/profiles/pat", nil); st != 200 || out["profile"].(map[string]any)["bio"] != "Gardener" {
		t.Fatalf("GET /profiles/pat as member = %d %v", st, out)
	}
	if st, out, _ := sam.api("GET", "/me", nil); st != 200 || out["user"].(map[string]any)["profile_id"] == "" {
		t.Fatalf("GET /me should carry profile_id: %d %v", st, out)
	}
	// An unknown address is a 404, not the sign-in page.
	status, body, _ = sam.get("/profiles/nobody")
	expect(t, "unknown profile", status, 404, body, "not found")

	// Public: everyone sees it.
	db.SetSetting(data.SettingProfileVisibility, "public")
	status, body, _ = visitor.get("/profiles/pat")
	expect(t, "profile public", status, 200, body, "profile Pat [pat]")
	status, body, _ = visitor.get("/")
	expect(t, "collections.profiles public", status, 200, body, "people: pat sam;")
	if st, out, _ := visitor.api("GET", "/profiles", nil); st != 200 || len(out["profiles"].([]any)) != 2 {
		t.Fatalf("GET /profiles public = %d %v", st, out)
	}
}

func TestProfileVisibilityFrozenByToml(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	_, srv := profileSite(t, "[site]\nname = \"p\"\n\n[settings]\nprofile_visibility = \"public\"\n\n[profiles.fields]\npronouns = \"text\"\nwebsite = { kind = \"text\", hint = \"https://…\" }\ntitle = \"text\"\n")
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	st, out, _ := sam.api("GET", "/profiles", nil)
	if st != 200 || out["visibility"] != "public" {
		t.Fatalf("GET /profiles = %d %v, want public from friendo.toml", st, out)
	}
	fields := out["fields"].([]any)
	if len(fields) != 3 || fields[0].(map[string]any)["name"] != "pronouns" || fields[1].(map[string]any)["hint"] != "https://…" {
		t.Fatalf("declared profile fields = %v", fields)
	}
	// Editing my own profile through the API: address, bio, declared data;
	// reserved keys in data are dropped, a taken address is a 409.
	st, me, _ := sam.api("GET", "/me", nil)
	authorID := me["user"].(map[string]any)["profile_id"].(string)
	st, out, _ = sam.api("PUT", "/me/profiles/"+authorID, map[string]any{
		"bio": "Hello", "slug": "Sam Q", "fields": map[string]any{"pronouns": "she/her", "name": "hacked"},
	})
	if st != 200 {
		t.Fatalf("PUT profile = %d %v", st, out)
	}
	profile := out["profile"].(map[string]any)
	if profile["slug"] != "sam-q" || profile["bio"] != "Hello" || profile["name"] != "sam" {
		t.Fatalf("profile after edit = %v", profile)
	}
	if d := profile["fields"].(map[string]any); d["pronouns"] != "she/her" || d["name"] != nil {
		t.Fatalf("profile data = %v", d)
	}
	pat := newBrowser(t, srv.URL)
	pat.signInMember("pat@test.com")
	st, me, _ = pat.api("GET", "/me", nil)
	patAuthor := me["user"].(map[string]any)["profile_id"].(string)
	if st, out, _ := pat.api("PUT", "/me/profiles/"+patAuthor, map[string]any{"slug": "sam-q"}); st != 409 {
		t.Fatalf("taking a used address = %d %v, want 409", st, out)
	}
	if st, out, _ := pat.api("PUT", "/me/profiles/"+authorID, map[string]any{"bio": "mine now"}); st != 404 {
		t.Fatalf("editing someone else's profile = %d %v, want 404", st, out)
	}
	if st, _, _ := pat.api("DELETE", "/me/profiles/"+patAuthor, nil); st != 409 {
		t.Fatalf("deleting the last profile = %d, want 409", st)
	}
	// The page reads the new address and the declared field.
	status, body, _ := pat.get("/profiles/sam-q")
	expect(t, "renamed profile", status, 200, body, "profile sam [sam-q] bio=Hello")
}

func strPtr(s string) *string { return &s }
