package data

import (
	"testing"
	"time"
)

func visitorSite(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Visitors share the empty email, and have no profile page or address.
func TestCreateVisitor(t *testing.T) {
	db := visitorSite(t)
	a, err := db.CreateVisitor()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateVisitor(); err != nil {
		t.Fatalf("a second visitor must fit beside the first: %v", err)
	}
	if !a.IsVisitor() || RoleRank(a.Role) != 0 || RoleAtLeast(a.Role, "member") || ValidRole(a.Role) {
		t.Fatalf("a visitor must hold no rank or role: %+v", a)
	}
	if db.DefaultAuthorID(a.ID) == "" {
		t.Fatal("a visitor needs a profile to act through")
	}
	if err := db.EnsureAuthorSlugs(); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListProfiles(); len(list) != 0 {
		t.Fatalf("visitors are not listed as profiles: %v", list)
	}
	if _, err := db.ProfileByID(db.DefaultAuthorID(a.ID)); err == nil {
		t.Fatal("a visitor's profile must not resolve as a profile")
	}
	// Members still can't share an email.
	if _, err := db.CreateMember("x@test.com", "", "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMember("X@test.com", "", "member"); err == nil {
		t.Fatal("two accounts with one email")
	}
}

func TestSetVisitorName(t *testing.T) {
	db := visitorSite(t)
	if _, err := db.CreateMember("sam@test.com", "Sam", "member"); err != nil {
		t.Fatal(err)
	}
	v, _ := db.CreateVisitor()
	if err := db.SetVisitorName(v.ID, " sam "); err != ErrNameTaken {
		t.Fatalf("a member's name must be refused, got %v", err)
	}
	if err := db.SetVisitorName(v.ID, "   "); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := db.SetVisitorName(v.ID, "Robin"); err != nil {
		t.Fatal(err)
	}
	if got := db.VisitorName(v.ID); got != "Robin" {
		t.Fatalf("name = %q", got)
	}
}

// A new email turns the visitor account into a member account in place.
func TestPromoteVisitor(t *testing.T) {
	db := visitorSite(t)
	v, _ := db.CreateVisitor()
	db.SetVisitorName(v.ID, "Robin")
	author := db.DefaultAuthorID(v.ID)
	db.ToggleReaction("post", "p1", author, "👍")
	token, _ := db.CreateVisitorSession(v.ID, "", "")

	u, err := db.PromoteVisitor(v.ID, "Robin@Test.com", "member")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != v.ID || u.Email != "robin@test.com" || u.Role != "member" || u.IsVisitor() {
		t.Fatalf("promoted = %+v", u)
	}
	p, err := db.ProfileByID(author)
	if err != nil || p["name"] != "Robin" || p["slug"] != "robin" {
		t.Fatalf("profile = %v, %v", p, err)
	}
	if list, _ := db.ReactionCounts("post", "p1", author); len(list) != 1 || list[0]["reacted"] != true {
		t.Fatalf("the reaction should still be theirs: %v", list)
	}
	if _, err := db.ValidateSession(token); err == nil {
		t.Fatal("the visitor's long session must not survive as a member session")
	}
	if _, err := db.PromoteVisitor(v.ID, "again@test.com", "member"); err == nil {
		t.Fatal("only a visitor can be promoted")
	}
}

// An existing account takes the visitor's activity; where both did the same
// thing, the account's own row wins.
func TestMergeVisitor(t *testing.T) {
	db := visitorSite(t)
	m, _ := db.CreateMember("m@test.com", "M", "member")
	mine := db.DefaultAuthorID(m.ID)
	v, _ := db.CreateVisitor()
	theirs := db.DefaultAuthorID(v.ID)
	db.CreateVisitorSession(v.ID, "", "")

	poll, err := db.CreatePoll("", "fav", "Favourite?", []string{"a", "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	db.ToggleReaction("post", "p1", mine, "👍")
	db.CastVote(poll, mine, 0)
	db.ToggleReaction("post", "p1", theirs, "👍")
	db.ToggleReaction("post", "p1", theirs, "❤️")
	db.CastVote(poll, theirs, 1)

	if err := db.MergeVisitor(v.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	counts, _ := db.ReactionCounts("post", "p1", mine)
	got := map[string]any{}
	for _, c := range counts {
		got[c["emoji"].(string)] = c["count"]
		if c["reacted"] != true {
			t.Errorf("%v should be the member's now", c["emoji"])
		}
	}
	if got["👍"] != 1 || got["❤️"] != 1 {
		t.Fatalf("counts = %v (the shared 👍 must count once)", got)
	}
	p, _ := db.GetPoll(poll, mine)
	if p["total_votes"] != 1 || p["my_vote"] != 0 {
		t.Fatalf("the member's vote wins: %v", p)
	}
	if _, err := db.GetUserByID(v.ID); err == nil {
		t.Fatal("the visitor account should be gone")
	}
	if err := db.MergeVisitor(m.ID, m.ID); err == nil {
		t.Fatal("only a visitor can be merged away")
	}
}

// Visitors with nothing to show are cleared after a while; ones with activity stay.
func TestClearIdleVisitors(t *testing.T) {
	db := visitorSite(t)
	idle, _ := db.CreateVisitor()
	busy, _ := db.CreateVisitor()
	fresh, _ := db.CreateVisitor()
	db.ToggleReaction("post", "p1", db.DefaultAuthorID(busy.ID), "👍")
	old := time.Now().UTC().Add(-2 * visitorIdleAge).Format("2006-01-02T15:04:05Z")
	db.Conn.Exec(`UPDATE users SET created = ? WHERE id IN (?, ?)`, old, idle.ID, busy.ID)

	if err := db.ClearIdleVisitors(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetUserByID(idle.ID); err == nil {
		t.Error("an old visitor with nothing to show should be cleared")
	}
	if _, err := db.GetUserByID(busy.ID); err != nil {
		t.Error("a visitor with activity must be kept")
	}
	if _, err := db.GetUserByID(fresh.ID); err != nil {
		t.Error("a new visitor must be kept")
	}
}
