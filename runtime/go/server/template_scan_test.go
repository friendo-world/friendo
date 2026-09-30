package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// A drop-box form in a template opens its collection; the same markup in a
// post body doesn't; taking the attribute away closes it on reload.
func TestDropBoxFromMarkup(t *testing.T) {
	siteDir := t.TempDir()
	pages := filepath.Join(siteDir, "pages")
	os.MkdirAll(filepath.Join(pages, "blog"), 0o755)
	os.MkdirAll(filepath.Join(siteDir, "layouts"), 0o755)
	tips := filepath.Join(pages, "tips.html")
	os.WriteFile(tips, []byte(`<friendo-form collection="tips" drop-box><textarea name="body"></textarea></friendo-form>
<friendo-chat chat-id="tips-chat" visitors-can-chat></friendo-chat>`), 0o644)
	os.WriteFile(filepath.Join(siteDir, "layouts", "base.html"), []byte(`<footer><friendo-form collection="feedback" drop-box></friendo-form><friendo-form collection="groups" drop-box></friendo-form></footer>`), 0o644)
	os.WriteFile(filepath.Join(pages, "blog", "[slug].html"), []byte(`{{ post.body|safe }}`), 0o644)

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A published post whose body carries the markup, rendered on a page.
	db.CreateRecord("blog", "sneaky", "Sneaky", `<friendo-form collection="blog" drop-box></friendo-form>`, "published", "")

	site, err := BuildSite(siteDir, db, false)
	if err != nil {
		t.Fatal(err)
	}
	post := func(collection string) int {
		req := httptest.NewRequest("POST", "/_/api/collections/"+collection+"/posts", strings.NewReader(`{"body":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.9:1"
		rec := httptest.NewRecorder()
		site.Handler.ServeHTTP(rec, req)
		return rec.Code
	}
	get := httptest.NewRecorder()
	site.Handler.ServeHTTP(get, httptest.NewRequest("GET", "/blog/sneaky", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("rendering the post = %d", get.Code)
	}

	if got := db.VisitorChatPatterns(); len(got) != 1 || got[0] != "tips-chat" {
		t.Fatalf("visitor chats = %v", got)
	}
	if code := post("tips"); code != http.StatusCreated {
		t.Fatalf("tips (a page's form) = %d, want 201", code)
	}
	if code := post("feedback"); code != http.StatusCreated {
		t.Fatalf("feedback (a layout's form) = %d, want 201", code)
	}
	if code := post("blog"); code != http.StatusUnauthorized {
		t.Fatalf("blog (only a post body says so) = %d, want 401", code)
	}

	os.WriteFile(tips, []byte(`<friendo-form collection="tips"></friendo-form>`), 0o644)
	site.Reload()
	if code := post("tips"); code != http.StatusUnauthorized {
		t.Fatalf("tips after the attribute is gone = %d, want 401", code)
	}
	if got := db.PageDropBoxes(); len(got) != 1 || got[0] != "feedback" {
		t.Fatalf("page drop boxes = %v (groups can never be one)", got)
	}
	if got := db.VisitorChatPatterns(); len(got) != 0 {
		t.Fatalf("the chat's attribute went with the page's rewrite: %v", got)
	}
	if got := db.ClosedDropBoxes(); len(got) != 1 || got[0] != "tips" {
		t.Fatalf("tips should be remembered as no longer a drop box: %v", got)
	}
}
