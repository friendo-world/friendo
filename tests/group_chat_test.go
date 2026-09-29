// Covers group chats: a page naming <friendo-chat chat-id="…" group="…"> makes
// the group's chat (two groups may share a key), reading/streaming/posting need
// membership (401 signed out, 403 outsider), moderators run the group's chats
// and may delete any message in them, editors count as members, GET /chats
// lists site chats only, and deleting the group takes its chats along.
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

func groupChatSite(t *testing.T) (*data.DB, *httptest.Server) {
	t.Helper()
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"gc\"\n\n[settings]\nmembers_can_start_groups = true\n")
	write("pages/groups/[slug].html", "{{ group.title }} chats:{% for c in group.chats %} {{ c.id }}{% endfor %} <friendo-chat chat-id=\"general\" group=\"{{ group.slug }}\"></friendo-chat>")
	write("pages/chat.html", "<friendo-chat chat-id=\"lobby\"></friendo-chat>")
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

func TestGroupChats(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	db, srv := groupChatSite(t)
	pat := newBrowser(t, srv.URL)
	pat.signInMember("pat@test.com")
	sam := newBrowser(t, srv.URL)
	sam.signInMember("sam@test.com")
	visitor := newBrowser(t, srv.URL)

	// Pat founds two public groups; rendering their pages makes each one's "general".
	for _, g := range []string{"board", "club"} {
		st, out, _ := pat.api("POST", "/collections/groups/posts", map[string]any{"title": g, "slug": g, "status": "published"})
		if st != 201 {
			t.Fatalf("create %s = %d %v", g, st, out)
		}
	}
	status, page, _ := pat.get("/groups/board")
	expect(t, "first render", status, 200, page, "board chats: ")
	status, page, _ = pat.get("/groups/board")
	expect(t, "second render lists the chat", status, 200, page, "board chats: general")
	pat.get("/groups/club")
	visitor.get("/chat") // makes the site-wide lobby

	// GET /chats lists site chats only.
	st, out, _ := visitor.api("GET", "/chats", nil)
	if st != 200 || len(out["chats"].([]any)) != 1 || out["chats"].([]any)[0].(map[string]any)["id"] != "lobby" {
		t.Fatalf("GET /chats = %d %v", st, out)
	}
	// The group's chats, and the group JSON carries them.
	if st, out, _ := pat.api("GET", "/groups/board/chats", nil); st != 200 || len(out["chats"].([]any)) != 1 {
		t.Fatalf("GET /groups/board/chats = %d %v", st, out)
	}
	if _, out, _ := pat.api("GET", "/groups/board", nil); len(out["group"].(map[string]any)["chats"].([]any)) != 1 {
		t.Fatalf("group JSON chats = %v", out)
	}

	// Membership rules on the room.
	if st, _, _ := visitor.api("GET", "/groups/board/chats/general/messages", nil); st != 401 {
		t.Fatalf("visitor reading a group chat = %d, want 401", st)
	}
	if st, _, _ := sam.api("GET", "/groups/board/chats/general/messages", nil); st != 403 {
		t.Fatalf("outsider reading a group chat = %d, want 403", st)
	}
	if st, _, _ := sam.api("POST", "/groups/board/chats/general/messages", map[string]any{"body": "hi"}); st != 403 {
		t.Fatalf("outsider posting = %d, want 403", st)
	}
	if st, out, _ := pat.api("POST", "/groups/board/chats/general/messages", map[string]any{"body": "Welcome"}); st != 201 {
		t.Fatalf("moderator posting = %d %v", st, out)
	}
	if st, out, _ := pat.api("GET", "/groups/board/chats/general/messages", nil); st != 200 || out["can_moderate"] != true || len(out["messages"].([]any)) != 1 {
		t.Fatalf("moderator reading = %d %v", st, out)
	}
	// The room is not reachable through the site-wide routes.
	boardChat, _ := db.GetGroupChat(func() string { g, _ := db.GroupBySlug("board"); return g["id"].(string) }(), "general")
	if st, _, _ := visitor.api("GET", "/chats/"+data.ChatRowID(boardChat)+"/messages", nil); st != 404 {
		t.Fatalf("group chat via site route = %d, want 404", st)
	}
	if st, _, _ := visitor.api("GET", "/chats/nope/stream", nil); st != 404 {
		t.Fatalf("stream of an unknown chat = %d, want 404", st)
	}

	// Sam joins: reads and posts, can't moderate; Pat (moderator) deletes Sam's message.
	if st, _, _ := sam.api("POST", "/groups/board/join", nil); st != 200 {
		t.Fatal("join")
	}
	st, out, _ = sam.api("GET", "/groups/board/chats/general/messages", nil)
	if st != 200 || out["can_moderate"] != false {
		t.Fatalf("member reading = %d %v", st, out)
	}
	st, posted, _ := sam.api("POST", "/groups/board/chats/general/messages", map[string]any{"body": "Hello"})
	if st != 201 {
		t.Fatalf("member posting = %d %v", st, posted)
	}
	samMsg := posted["message"].(map[string]any)["id"].(string)
	patMsg := out["messages"].([]any)[0].(map[string]any)["id"].(string)
	if st, _, _ := sam.api("DELETE", "/messages/"+patMsg, nil); st != 403 {
		t.Fatalf("member deleting a moderator's message = %d, want 403", st)
	}
	if st, _, _ := pat.api("DELETE", "/messages/"+samMsg, nil); st != 204 {
		t.Fatalf("moderator deleting a member's message = %d, want 204", st)
	}
	// The club's "general" is a different room.
	if _, out, _ := pat.api("GET", "/groups/club/chats/general/messages", nil); len(out["messages"].([]any)) != 0 {
		t.Fatalf("club general should be empty: %v", out)
	}

	// Moderators add and remove chats; members can't.
	if st, _, _ := sam.api("POST", "/groups/board/chats", map[string]any{"name": "Plans"}); st != 403 {
		t.Fatalf("member adding a chat = %d, want 403", st)
	}
	if st, out, _ := pat.api("POST", "/groups/board/chats", map[string]any{"name": "Plans"}); st != 201 || len(out["chats"].([]any)) != 2 {
		t.Fatalf("moderator adding a chat = %d %v", st, out)
	}
	if st, _, _ := pat.api("POST", "/groups/board/chats", map[string]any{"id": "plans"}); st != 409 {
		t.Fatalf("duplicate chat id = %d, want 409", st)
	}
	if st, _, _ := pat.api("POST", "/groups/board/chats", map[string]any{"key": "bad key"}); st != 400 {
		t.Fatalf("bad key = %d, want 400", st)
	}
	status, page, _ = sam.get("/groups/board")
	if !strings.Contains(page, "general") || !strings.Contains(page, "plans") {
		t.Fatalf("post.chats = %q", page)
	}
	if st, _, _ := pat.api("DELETE", "/groups/board/chats/plans", nil); st != 204 {
		t.Fatalf("remove chat = %d", st)
	}
	// An editor is a member of every group's chats.
	ed, _ := db.CreateMember("ed@test.com", "Ed", "editor")
	_ = ed
	editor := newBrowser(t, srv.URL)
	editor.signInMember("ed@test.com")
	if st, out, _ := editor.api("GET", "/groups/board/chats/general/messages", nil); st != 200 || out["can_moderate"] != true {
		t.Fatalf("editor reading = %d %v", st, out)
	}
	// Deleting the group (an editor may) takes its chats along.
	board, _ := db.GroupBySlug("board")
	if st, _, _ := editor.api("DELETE", "/posts/"+board["id"].(string), nil); st != 204 {
		t.Fatalf("delete group = %d", st)
	}
	if _, err := db.GetChat(boardChat["id"].(string)); err == nil {
		t.Fatal("the group's chat should be gone")
	}
	// With chats off, the group's rooms are off too.
	db.SetSetting(data.FeatureSetting("chats"), "false")
	if st, out, _ := pat.api("GET", "/groups/club/chats", nil); st != 403 || out["off"] != true {
		t.Fatalf("chats off = %d %v", st, out)
	}
}
