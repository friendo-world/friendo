// A site is one big group: a moderator keeps it tidy without editing what
// others wrote. They may approve or reject a post waiting for review (and see
// the queue), but not edit someone else's post, publish a draft, or change
// settings; a contributor can't touch the queue at all.
package tests

import (
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

func TestSiteModeratorReviewsButDoesNotEdit(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	siteDir := t.TempDir()
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	owner := newAPIClient(t, db, siteDir)
	st, out := owner.do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})
	owner.want(201, st, out, "setup")
	st, out = owner.do("PUT", "/settings", map[string]any{"posts_need_review": true})
	owner.want(200, st, out, "posts need review")
	for _, u := range []map[string]any{
		{"email": "mod@test.com", "name": "Mo", "password": "password12345", "role": "moderator"},
		{"email": "con@test.com", "name": "Cee", "password": "password12345", "role": "contributor"},
	} {
		st, out = owner.do("POST", "/users", u)
		owner.want(201, st, out, "create "+u["role"].(string))
	}

	// A contributor's post waits for review.
	con := newAPIClient(t, db, siteDir)
	st, out = con.do("POST", "/auth/login", map[string]any{"email": "con@test.com", "password": "password12345"})
	con.want(200, st, out, "contributor login")
	st, out = con.do("POST", "/collections/blog/posts", map[string]any{"title": "Mine", "body": "hi", "status": "published"})
	con.want(201, st, out, "contributor posts")
	rec := out["post"].(map[string]any)
	if rec["status"] != "pending" {
		t.Fatalf("contributor's post status = %v, want pending", rec["status"])
	}
	id := rec["id"].(string)
	st, out = con.do("GET", "/posts?status=pending", nil)
	con.want(403, st, out, "a contributor can't see the review queue")

	mod := newAPIClient(t, db, siteDir)
	st, out = mod.do("POST", "/auth/login", map[string]any{"email": "mod@test.com", "password": "password12345"})
	mod.want(200, st, out, "moderator login")
	st, out = mod.do("GET", "/posts?status=pending", nil)
	mod.want(200, st, out, "moderator sees the queue")
	if n := len(out["posts"].([]any)); n != 1 {
		t.Fatalf("queue length = %d, want 1", n)
	}
	st, out = mod.do("PUT", "/posts/"+id, map[string]any{"title": "Edited by mod", "body": "hi"})
	mod.want(403, st, out, "moderator can't edit another's post")
	st, out = mod.do("PUT", "/posts/"+id+"/status", map[string]any{"status": "published"})
	mod.want(200, st, out, "moderator approves")
	if out["post"].(map[string]any)["status"] != "published" {
		t.Fatalf("after approval: %v", out)
	}
	st, out = mod.do("PUT", "/posts/"+id+"/status", map[string]any{"status": "draft"})
	mod.want(403, st, out, "moderator can't unpublish a live post")
	st, out = mod.do("DELETE", "/posts/"+id, nil)
	mod.want(403, st, out, "moderator can't delete a live post")
	st, out = mod.do("PUT", "/settings", map[string]any{"posts_need_review": false})
	mod.want(403, st, out, "moderator can't change settings")

	// Rejecting: a second pending post goes back to draft, or out entirely.
	st, out = con.do("POST", "/collections/blog/posts", map[string]any{"title": "Again", "body": "hi"})
	con.want(201, st, out, "second post")
	id2 := out["post"].(map[string]any)["id"].(string)
	st, out = mod.do("PUT", "/posts/"+id2+"/status", map[string]any{"status": "draft"})
	mod.want(200, st, out, "moderator rejects to draft")
	st, out = con.do("POST", "/collections/blog/posts", map[string]any{"title": "Third", "body": "hi"})
	con.want(201, st, out, "third post")
	id3 := out["post"].(map[string]any)["id"].(string)
	st, out = mod.do("DELETE", "/posts/"+id3, nil)
	if st != 204 {
		t.Fatalf("moderator deletes a pending post: %d %v", st, out)
	}
	// Moderators moderate comments everywhere.
	st, out = mod.do("GET", "/comments?status=pending", nil)
	mod.want(200, st, out, "moderator lists all comments")
}
