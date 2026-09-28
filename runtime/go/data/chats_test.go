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
