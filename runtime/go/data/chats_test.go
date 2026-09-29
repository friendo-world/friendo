package data

import "testing"

// A message whose author row is gone (or was never there) must still list for
// a signed-in viewer: `mine` is false, not NULL.
func TestListMessagesWithoutAuthorForSignedInViewer(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.EnsureChat("general"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := db.CreateMessage("general", "", "", "orphan"); err != nil {
		t.Fatalf("create: %v", err)
	}
	msgs, err := db.ListMessages("general", "some-user")
	if err != nil {
		t.Fatalf("a message with no author should still list for a signed-in viewer: %v", err)
	}
	if len(msgs) != 1 || msgs[0]["mine"] != false {
		t.Fatalf("want one message that isn't mine, got %v", msgs)
	}
}

// ChatIDsInHTML picks out valid, distinct ids in order.
func TestChatIDsInHTML(t *testing.T) {
	html := `<friendo-chat chat-id="general"></friendo-chat>
<FRIENDO-CHAT class="x" chat-id='event-42'></FRIENDO-CHAT>
<friendo-chat chat-id="general"></friendo-chat>
<friendo-chat chat-id="not ok"></friendo-chat>
<friendo-chat></friendo-chat>`
	got := ChatIDsInHTML(html)
	if len(got) != 2 || got[0] != "general" || got[1] != "event-42" {
		t.Fatalf("got %v", got)
	}
}

// ChatRefsInHTML reads chat-id and group in either order, dedupes, and drops
// bad slugs; GroupChatID is stable so re-registering is one row.
func TestChatRefsInHTMLAndGroupChats(t *testing.T) {
	html := `<friendo-chat chat-id="general"></friendo-chat>
	<friendo-chat group="board" chat-id="general"></friendo-chat>
	<friendo-chat chat-id='plans' group='board'></friendo-chat>
	<friendo-chat chat-id="general" group="board"></friendo-chat>
	<friendo-chat chat-id="bad id" group="board"></friendo-chat>
	<friendo-chat chat-id="x" group="no/slash"></friendo-chat>`
	refs := ChatRefsInHTML(html)
	want := []ChatRef{{Key: "general"}, {Key: "general", Group: "board"}, {Key: "plans", Group: "board"}}
	if len(refs) != len(want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("refs[%d] = %v, want %v", i, refs[i], want[i])
		}
	}
	if ids := ChatIDsInHTML(html); len(ids) != 1 || ids[0] != "general" {
		t.Fatalf("ChatIDsInHTML = %v", ids)
	}
	if GroupChatID("g1", "general") != GroupChatID("g1", "general") || GroupChatID("g1", "general") == GroupChatID("g2", "general") {
		t.Fatal("GroupChatID should be stable per (group, key)")
	}

	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	made, err := db.EnsureGroupChat("g1", "general")
	if err != nil || !made {
		t.Fatalf("EnsureGroupChat = %v %v", made, err)
	}
	if made, _ := db.EnsureGroupChat("g1", "general"); made {
		t.Fatal("second EnsureGroupChat should be a no-op")
	}
	if made, _ := db.EnsureGroupChat("g2", "general"); !made {
		t.Fatal("another group may have its own general")
	}
	db.EnsureChat("general") // a site-wide one too
	site, _ := db.ListChats()
	if len(site) != 1 || site[0]["id"] != "general" || site[0]["group_id"] != "" {
		t.Fatalf("ListChats should list site chats only: %v", site)
	}
	g1, _ := db.ListGroupChats("g1")
	if len(g1) != 1 || g1[0]["id"] != "general" || g1[0]["group_id"] != "g1" {
		t.Fatalf("ListGroupChats(g1) = %v", g1)
	}
	if _, err := db.CreateGroupChat("g1", "general", ""); err == nil {
		t.Fatal("a duplicate key in a group must fail")
	}
	if _, err := db.CreateGroupChat("g1", "plans", "Plans"); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetGroupChat("g1", "plans")
	if err != nil || c["name"] != "Plans" {
		t.Fatalf("GetGroupChat = %v %v", c, err)
	}
	db.CreateMessage(ChatRowID(c), "", "a1", "hi")
	if err := db.DeleteChatsForGroup("g1"); err != nil {
		t.Fatal(err)
	}
	if left, _ := db.ListGroupChats("g1"); len(left) != 0 {
		t.Fatalf("chats left after cascade: %v", left)
	}
	if msgs, _ := db.ListMessages(ChatRowID(c), ""); len(msgs) != 0 {
		t.Fatalf("messages left after cascade: %v", msgs)
	}
}
