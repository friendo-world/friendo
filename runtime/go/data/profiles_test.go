package data

import (
	"errors"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Pat O'Brien":      "pat-o-brien",
		"  Ana  María ":    "ana-maría",
		"---":              "",
		"":                 "",
		"Hello, World! 42": "hello-world-42",
		"ALREADY-a-slug":   "already-a-slug",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	long := Slugify("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if len(long) != 64 {
		t.Errorf("Slugify should cap at 64 characters, got %d", len(long))
	}
}

// A database from before 0016 gets addresses derived from names on open, and two
// people called Pat don't collide.
func TestEnsureAuthorSlugsBackfillsAndDisambiguates(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"a1", "a2", "a3"} {
		if _, err := db.Conn.Exec(`INSERT INTO authors (id, site_id, name, slug) VALUES (?, ?, 'Pat', '')`, id, db.SiteID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.EnsureAuthorSlugs(); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, id := range []string{"a1", "a2", "a3"} {
		p, err := db.ProfileByID(id)
		if err != nil {
			t.Fatal(err)
		}
		got[id] = p["slug"].(string)
	}
	if got["a1"] != "pat" || got["a2"] != "pat-2" || got["a3"] != "pat-3" {
		t.Fatalf("slugs = %v, want pat, pat-2, pat-3", got)
	}
}

func TestProfileSlugsAndEditing(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, err := db.CreateMember("pat@test.com", "Pat", "member")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateMember("sam@test.com", "Sam", "member")
	if err != nil {
		t.Fatal(err)
	}
	pat := db.DefaultAuthorID(u.ID)
	if p, _ := db.ProfileByID(pat); p["slug"] != "pat" || p["url"] != "/profiles/pat" {
		t.Fatalf("new member's profile = %v", p)
	}

	// A second profile with the same name gets a numbered address.
	p2, err := db.CreateProfile(u.ID, "Pat", "")
	if err != nil {
		t.Fatal(err)
	}
	if p2["slug"] != "pat-2" {
		t.Fatalf("second Pat slug = %v", p2["slug"])
	}

	// Editing: bio, data, a new address.
	bio, slug := "Hi there", "patrick"
	got, err := db.UpdateProfile(u.ID, pat, ProfileUpdate{Bio: &bio, Slug: &slug, Data: map[string]any{"pronouns": "they/them"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["slug"] != "patrick" || got["bio"] != "Hi there" || got["fields"].(map[string]any)["pronouns"] != "they/them" {
		t.Fatalf("updated = %v", got)
	}
	if _, err := db.ProfileBySlug("patrick"); err != nil {
		t.Fatalf("ProfileBySlug after rename: %v", err)
	}
	// Keeping your own slug is never a collision; taking someone else's is.
	if _, err := db.UpdateProfile(u.ID, pat, ProfileUpdate{Slug: &slug}); err != nil {
		t.Fatalf("re-saving own slug: %v", err)
	}
	taken := "sam"
	if _, err := db.UpdateProfile(u.ID, pat, ProfileUpdate{Slug: &taken}); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("taking sam's slug: err = %v, want ErrSlugTaken", err)
	}
	// Only the owner may edit.
	if _, err := db.UpdateProfile(other.ID, pat, ProfileUpdate{Bio: &bio}); err == nil {
		t.Fatal("another account edited my profile")
	}

	// Deleting: never the last one.
	if err := db.DeleteProfile(u.ID, p2["id"].(string)); err != nil {
		t.Fatalf("delete second profile: %v", err)
	}
	if err := db.DeleteProfile(u.ID, pat); !errors.Is(err, ErrLastProfile) {
		t.Fatalf("deleting the last profile: err = %v, want ErrLastProfile", err)
	}
}

func TestAttachAuthorsAndPostsByAuthor(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, _ := db.CreateMember("pat@test.com", "Pat", "contributor")
	pat := db.DefaultAuthorID(u.ID)
	if _, err := db.CreateRecord("blog", "hello", "Hello", "…", "published", pat); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateRecord("blog", "draft", "Draft", "…", "draft", pat); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateRecord("blog", "orphan", "Orphan", "…", "published", "nobody"); err != nil {
		t.Fatal(err)
	}
	records, _ := db.QueryPublishedCollection("blog")
	db.AttachAuthors(records)
	for _, r := range records {
		switch r["slug"] {
		case "hello":
			if a, _ := r["author"].(map[string]any); a == nil || a["slug"] != "pat" {
				t.Fatalf("hello.author = %v", r["author"])
			}
			if _, has := r["author"].(map[string]any)["email"]; has {
				t.Fatal("post.author must never carry the email")
			}
		case "orphan":
			if r["author"] != nil {
				t.Fatalf("orphan.author = %v, want nil", r["author"])
			}
		}
	}
	posts, err := db.PostsByAuthor(pat)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0]["slug"] != "hello" || posts[0]["collection"] != "blog" {
		t.Fatalf("PostsByAuthor = %v", posts)
	}
	if db.ProfileVisibility() != "members" || db.ProfilesVisibleTo(false) || !db.ProfilesVisibleTo(true) {
		t.Fatal("profiles should default to members-only")
	}
	db.SetSetting(SettingProfileVisibility, "public")
	if !db.ProfilesVisibleTo(false) {
		t.Fatal("public profiles should be visible signed out")
	}
}
