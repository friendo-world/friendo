package api

import (
	"net/http"
	"testing"
)

// A visitor writes only in chats a template marks visitors-can-chat, gives a
// name first, and can delete only their own messages.
func TestVisitorChat(t *testing.T) {
	s := newVisitorSite(t, "")
	for _, id := range []string{"general", "members", "post-hello"} {
		s.db.EnsureChat(id)
	}
	s.db.SetVisitorChatPatterns([]string{"general", "post-{{ post.slug }}"})

	rec, _ := s.do("GET", "/chats/general/messages", "", "")
	if d := decode(t, rec); d["can_post"] != true || d["visitors_can_chat"] != true {
		t.Fatalf("an open chat, seen by nobody yet = %v", d)
	}
	rec, _ = s.do("GET", "/chats/members/messages", "", "")
	if d := decode(t, rec); d["can_post"] != false {
		t.Fatalf("a members' chat = %v", d)
	}

	rec, visitor := s.do("POST", "/chats/general/messages", "", `{"body":"hi"}`)
	if rec.Code != http.StatusBadRequest || decode(t, rec)["needs_name"] != true {
		t.Fatalf("no name = %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = s.do("POST", "/chats/general/messages", visitor, `{"body":"hi","name":"Robin"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("message = %d %s", rec.Code, rec.Body.String())
	}
	mine := decode(t, rec)["message"].(map[string]any)
	if mine["author_name"] != "Robin (visitor)" {
		t.Fatalf("message = %v", mine)
	}
	if rec, _ := s.do("POST", "/chats/post-hello/messages", visitor, `{"body":"hi"}`); rec.Code != http.StatusCreated {
		t.Fatalf("a templated open chat = %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := s.do("POST", "/chats/members/messages", visitor, `{"body":"hi"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a members' chat = %d, want 401", rec.Code)
	}
	if rec, _ := s.do("GET", "/chats/members/messages", visitor, ""); decode(t, rec)["can_post"] != false {
		t.Fatal("a visitor must not be offered a members' chat")
	}
	if rec, _ := s.do("POST", "/chats/general/messages", visitor, `{"body":"buy","trap":"x"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("trapped = %d", rec.Code)
	}

	rec, _ = s.do("GET", "/chats/general/messages", visitor, "")
	d := decode(t, rec)
	msgs := d["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["mine"] != true || d["visitor_name"] != "Robin" || d["can_moderate"] != false {
		t.Fatalf("the visitor's view = %v", d)
	}

	_, member := s.signIn("m@test.com", "")
	rec, _ = s.do("POST", "/chats/general/messages", member, `{"body":"hello Robin"}`)
	theirs := decode(t, rec)["message"].(map[string]any)["id"].(string)
	if rec, _ := s.do("DELETE", "/messages/"+theirs, visitor, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("deleting a member's message = %d, want 403", rec.Code)
	}
	if rec, _ := s.do("DELETE", "/messages/"+mine["id"].(string), visitor, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting their own = %d", rec.Code)
	}
}
