package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// An upload on a post that isn't published is served only to whoever may
// review or edit it, or sent it; once the post is published, to everyone.
func TestUnpublishedUploadsArePrivate(t *testing.T) {
	siteDir := t.TempDir()
	os.MkdirAll(filepath.Join(siteDir, "pages"), 0o755)
	os.MkdirAll(filepath.Join(siteDir, "assets", "uploads"), 0o755)
	os.WriteFile(filepath.Join(siteDir, "assets", "site.css"), []byte("body{}"), 0o644)
	os.WriteFile(filepath.Join(siteDir, "assets", "uploads", "u1.png"), []byte("png"), 0o644)
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sender, _ := db.CreateMember("s@test.com", "S", "member")
	stranger, _ := db.CreateMember("x@test.com", "X", "member")
	mod, _ := db.CreateMember("m@test.com", "M", "moderator")
	post, _ := db.CreateRecord("tips", "t", "T", "", "pending", db.DefaultAuthorID(sender.ID))
	if _, err := db.CreateFileWithID("u1", "post", post, "photo", "assets/uploads/u1.png", "image/png", 3); err != nil {
		t.Fatal(err)
	}
	visitor, _ := db.CreateVisitor()
	vpost, _ := db.CreateRecord("tips", "v", "V", "", "pending", db.DefaultAuthorID(visitor.ID))
	os.WriteFile(filepath.Join(siteDir, "assets", "uploads", "u2.png"), []byte("png"), 0o644)
	db.CreateFileWithID("u2", "post", vpost, "photo", "assets/uploads/u2.png", "image/png", 3)

	handler, err := BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, user *data.User) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if user != nil {
			var token string
			if user.IsVisitor() {
				token, _ = db.CreateVisitorSession(user.ID, "", "")
			} else {
				token, _ = db.CreateSession(user.ID, "", "")
			}
			req.AddCookie(&http.Cookie{Name: "friendo_session", Value: token})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for _, c := range []struct {
		name string
		path string
		user *data.User
		want int
	}{
		{"nobody", "/assets/uploads/u1.png", nil, http.StatusNotFound},
		{"another member", "/assets/uploads/u1.png", stranger, http.StatusNotFound},
		{"the sender", "/assets/uploads/u1.png", sender, http.StatusOK},
		{"a moderator", "/assets/uploads/u1.png", mod, http.StatusOK},
		{"the visitor who sent it", "/assets/uploads/u2.png", visitor, http.StatusOK},
		{"a site asset", "/assets/site.css", nil, http.StatusOK},
	} {
		rec := get(c.path, c.user)
		if rec.Code != c.want {
			t.Errorf("%s: %s = %d, want %d", c.name, c.path, rec.Code, c.want)
		}
		if c.want == http.StatusOK && c.user != nil && rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: an unpublished upload must not be cached publicly: %q", c.name, rec.Header().Get("Cache-Control"))
		}
	}

	db.SetRecordStatus(post, "published")
	if rec := get("/assets/uploads/u1.png", nil); rec.Code != http.StatusOK {
		t.Fatalf("published = %d, want 200", rec.Code)
	}
	if keys := db.UnpublishedUploadKeys(); len(keys) != 1 || keys[0] != "assets/uploads/u2.png" {
		t.Fatalf("unpublished keys = %v", keys)
	}
}
