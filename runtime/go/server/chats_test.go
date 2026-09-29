package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// A page that names a chat makes it: serving <friendo-chat chat-id="…"> registers
// the chat the first time, including an id a template computes per record.
func TestPageNamingAChatRegistersIt(t *testing.T) {
	siteDir := t.TempDir()
	pages := filepath.Join(siteDir, "pages")
	os.MkdirAll(filepath.Join(pages, "blog"), 0o755)
	os.WriteFile(filepath.Join(pages, "index.html"),
		[]byte(`<friendo-chat chat-id="general"></friendo-chat>
<friendo-chat chat-id='general'></friendo-chat>
<friendo-chat chat-id="not a slug"></friendo-chat>`), 0o644)
	os.WriteFile(filepath.Join(pages, "blog", "[slug].html"),
		[]byte(`<h1>{{ post.title }}</h1><friendo-chat chat-id="post-{{ post.slug }}"></friendo-chat>`), 0o644)
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.CreateRecord("blog", "hello", "Hello", "", "published", ""); err != nil {
		t.Fatalf("record: %v", err)
	}
	h, err := BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatalf("BuildSiteHandler: %v", err)
	}
	get := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code
	}

	if chats, _ := db.ListChats(); len(chats) != 0 {
		t.Fatalf("a fresh site should have no chats, got %v", chats)
	}
	if code := get("/"); code != http.StatusOK {
		t.Fatalf("GET /: %d", code)
	}
	chats, _ := db.ListChats()
	if len(chats) != 1 || chats[0]["id"] != "general" || chats[0]["name"] != "general" {
		t.Fatalf("serving the home page should register exactly `general` (not the invalid id, not twice), got %v", chats)
	}
	if code := get("/"); code != http.StatusOK {
		t.Fatalf("GET / again: %d", code)
	}
	if chats, _ := db.ListChats(); len(chats) != 1 {
		t.Fatalf("serving the page again should not make another chat, got %v", chats)
	}

	// A per-record chat id, computed by the template.
	if code := get("/blog/hello"); code != http.StatusOK {
		t.Fatalf("GET /blog/hello: %d", code)
	}
	if _, err := db.GetChat("post-hello"); err != nil {
		t.Fatalf("the post page should have registered post-hello: %v", err)
	}

	// Off means off: no new chats while the feature is switched off.
	db.SetSetting(data.FeatureSetting("chats"), "false")
	os.WriteFile(filepath.Join(pages, "index.html"), []byte(`<friendo-chat chat-id="later"></friendo-chat>`), 0o644)
	get("/")
	if _, err := db.GetChat("later"); err == nil {
		t.Fatal("a switched-off feature should not register chats")
	}
}
