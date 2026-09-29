package data

import (
	"testing"
	"time"
)

// An invitation is an RSVP row nobody has answered: it never overwrites a real
// answer, a member can't set it themselves, and the tally counts it apart.
func TestInviteRSVP(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pat, _ := db.CreateMember("pat@t.com", "Pat", "member")
	sam, _ := db.CreateMember("sam@t.com", "Sam", "member")
	patA, samA := db.DefaultAuthorID(pat.ID), db.DefaultAuthorID(sam.ID)
	post, _ := db.CreateRecord("events", "party", "Party", "", "published", patA)
	ev, _, present := ParseWhen(map[string]any{"when": "2027-06-01 19:00"}, nil)
	if !present {
		t.Fatal("ParseWhen")
	}
	if err := db.ReconcileWhen(post, ev); err != nil {
		t.Fatal(err)
	}
	ev = db.EventFor(post)
	key, _ := ev.ResolveOccurrence("", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))

	if err := db.SetRSVP(ev.ID, key, samA, RSVPInvited); err == nil {
		t.Fatal("a member must not be able to answer \"invited\"")
	}
	wrote, err := db.InviteRSVP(ev.ID, key, samA)
	if err != nil || !wrote {
		t.Fatalf("InviteRSVP = %v %v", wrote, err)
	}
	if wrote, _ := db.InviteRSVP(ev.ID, key, samA); wrote {
		t.Fatal("inviting twice should write nothing")
	}
	if got := db.RSVPForUser(ev.ID, key, sam.ID); got != RSVPInvited {
		t.Fatalf("RSVPForUser = %q", got)
	}
	c, _ := db.RSVPCountsFor(ev.ID, key)
	if c.Invited != 1 || c.Going != 0 {
		t.Fatalf("counts = %+v", c)
	}
	invited, _ := db.InvitedFor(ev.ID, key)
	if len(invited) != 1 || invited[0]["slug"] != "sam" {
		t.Fatalf("InvitedFor = %v", invited)
	}
	// Sam answers: the invitation becomes the answer.
	if err := db.SetRSVP(ev.ID, key, samA, "going"); err != nil {
		t.Fatal(err)
	}
	c, _ = db.RSVPCountsFor(ev.ID, key)
	if c.Invited != 0 || c.Going != 1 {
		t.Fatalf("counts after answer = %+v", c)
	}
	// Inviting someone who answered is a no-op that keeps their answer.
	if wrote, _ := db.InviteRSVP(ev.ID, key, samA); wrote {
		t.Fatal("must not overwrite a real answer")
	}
	if got := db.RSVPForUser(ev.ID, key, sam.ID); got != "going" {
		t.Fatalf("answer after re-invite = %q", got)
	}
	summary := db.RSVPSummary(ev, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if summary["going"] != 1 || summary["invited"] != 0 {
		t.Fatalf("RSVPSummary = %v", summary)
	}
}
