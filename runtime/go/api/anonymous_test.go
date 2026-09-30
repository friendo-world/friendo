package api

import (
	"net/http"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// memberSession makes an account with a role and returns its session and profile.
func (s *visitorSite) memberSession(email, role string) (string, string, *data.User) {
	s.t.Helper()
	u, err := s.db.CreateMember(email, "", role)
	if err != nil {
		s.t.Fatal(err)
	}
	token, _ := s.db.CreateSession(u.ID, "", "")
	return token, s.db.DefaultAuthorID(u.ID), u
}

func findComment(t *testing.T, s *visitorSite, postID, cookie, id string) map[string]any {
	t.Helper()
	rec, _ := s.do("GET", "/posts/"+postID+"/comments", cookie, "")
	for _, c := range decode(t, rec)["comments"].([]any) {
		if m := c.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("comment %s not listed for %q", id, cookie)
	return nil
}

// An anonymous comment shows its author only to its writer and to moderators —
// not to the public, and not to the author of the post it's on.
func TestAnonymousComment(t *testing.T) {
	s := newVisitorSite(t, "members_can_be_anonymous = true\ncomments_need_review = false")
	writer, _, _ := s.memberSession("w@test.com", "member")
	owner, ownerProfile, ownerUser := s.memberSession("o@test.com", "contributor")
	mod, _, _ := s.memberSession("m@test.com", "moderator")
	post, _ := s.db.CreateRecord("blog", "p", "P", "", "published", ownerProfile)

	rec, _ := s.do("POST", "/posts/"+post+"/comments", writer, `{"body":"psst","anonymous":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment = %d %s", rec.Code, rec.Body.String())
	}
	id := decode(t, rec)["comment"].(map[string]any)["id"].(string)

	for who, cookie := range map[string]string{"the public": "", "the post's author": owner} {
		c := findComment(t, s, post, cookie, id)
		if c["author_name"] != "Anonymous" || c["author_id"] != "" || c["author_avatar"] != "" {
			t.Errorf("%s sees %v", who, c)
		}
	}
	for who, cookie := range map[string]string{"the writer": writer, "a moderator": mod} {
		if c := findComment(t, s, post, cookie, id); c["author_name"] == "Anonymous" || c["anonymous"] != true {
			t.Errorf("%s should see who, marked anonymous: %v", who, c)
		}
	}
	// The post's author reviewing it gets no name back either.
	rec, _ = s.do("PUT", "/comments/"+id, owner, `{"status":"approved"}`)
	if c := decode(t, rec)["comment"].(map[string]any); c["author_name"] != "Anonymous" {
		t.Errorf("review response to the post's author = %v", c)
	}
	// Nor from their inbox.
	notes, _ := s.db.ListNotifications(ownerUser.ID, false, 10)
	if len(notes) == 0 {
		t.Fatal("the post's author should still hear about the comment")
	}
	for _, n := range notes {
		if n.ActorID != "" || n.Actor != nil {
			t.Errorf("the notification names who: %+v", n)
		}
	}
	// And the public page's comments are hidden at the source.
	list, _ := s.db.ListCommentsByPost(post, false)
	if len(list) != 1 || list[0]["author_name"] != "Anonymous" {
		t.Errorf("post page comments = %v", list)
	}
	if mine, _ := s.db.CommentsByAuthor(s.db.DefaultAuthorID(mustUser(t, s, "w@test.com").ID)); len(mine) != 0 {
		t.Errorf("an anonymous comment must not show on its writer's profile: %v", mine)
	}
}

func mustUser(t *testing.T, s *visitorSite, email string) *data.User {
	t.Helper()
	u, err := s.db.GetUserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// Asking for it where it isn't allowed posts nothing, rather than the name.
func TestAnonymousRefused(t *testing.T) {
	s := newVisitorSite(t, "members_can_post = true\nvisitors_can_comment = true")
	writer, _, _ := s.memberSession("w@test.com", "member")
	post, _ := s.db.CreateRecord("blog", "p", "P", "", "published", "")
	if rec, _ := s.do("POST", "/posts/"+post+"/comments", writer, `{"body":"x","anonymous":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous with the switch off = %d, want 403", rec.Code)
	}
	if rec, _ := s.do("POST", "/collections/stories/posts", writer, `{"title":"x","anonymous":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous post with the switch off = %d, want 403", rec.Code)
	}
	if list, _ := s.db.ListCommentsByStatus(""); len(list) != 0 {
		t.Fatalf("nothing should be saved: %v", list)
	}
	// A visitor who ticks it just isn't named.
	rec, visitor := s.do("POST", "/posts/"+post+"/comments", "", `{"body":"x","name":"Robin","anonymous":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("visitor = %d", rec.Code)
	}
	v, _ := s.db.ValidateSession(visitor)
	if s.db.VisitorName(v.ID) != "" {
		t.Fatal("a visitor asking to be anonymous shouldn't get the name saved")
	}

	s.db.SetSetting(settingMembersCanBeAnonymous, "true")
	if rec, _ := s.do("POST", "/collections/groups/posts", writer, `{"title":"g","anonymous":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("an anonymous group = %d, want 403", rec.Code)
	}
}

// An anonymous post has no author wherever the public sees it, and its author
// can switch it back.
func TestAnonymousPost(t *testing.T) {
	s := newVisitorSite(t, "members_can_be_anonymous = true")
	writer, profile, _ := s.memberSession("c@test.com", "contributor")
	rec, _ := s.do("POST", "/collections/blog/posts", writer, `{"title":"Quiet","status":"published","anonymous":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post = %d %s", rec.Code, rec.Body.String())
	}
	p := decode(t, rec)["post"].(map[string]any)
	id := p["id"].(string)
	if p["anonymous"] != true {
		t.Fatalf("post = %v", p)
	}

	published, _ := s.db.QueryPublishedCollection("blog")
	s.db.AttachAuthors(published)
	if len(published) != 1 || published[0]["author_id"] != "" || published[0]["author"] != nil || published[0]["anonymous"] != true {
		t.Fatalf("public listing = %v", published)
	}
	if byAuthor, _ := s.db.PostsByAuthor(profile); len(byAuthor) != 0 {
		t.Fatalf("an anonymous post must not show on its author's profile: %v", byAuthor)
	}
	s.db.SetRecordStatus(id, "pending")
	queue, _ := s.db.ListRecordsByStatus("pending")
	if len(queue) != 1 || queue[0]["author_id"] != profile || queue[0]["anonymous"] != true {
		t.Fatalf("moderators see who, marked: %v", queue)
	}

	// Its author (and only its author) can put their name back on it.
	other, _, _ := s.memberSession("e@test.com", "editor")
	s.do("PUT", "/posts/"+id, other, `{"title":"Quiet","anonymous":false}`)
	if !s.db.PostIsAnonymous(id) {
		t.Fatal("only the author says whether their name shows")
	}
	rec, _ = s.do("PUT", "/posts/"+id, writer, `{"title":"Quiet","anonymous":false}`)
	if rec.Code != http.StatusOK || s.db.PostIsAnonymous(id) {
		t.Fatalf("author un-anonymizing = %d %s", rec.Code, rec.Body.String())
	}
}
