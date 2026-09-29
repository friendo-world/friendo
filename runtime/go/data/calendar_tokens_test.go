package data

import "testing"

func TestCalendarTokens(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pat, _ := db.CreateMember("pat@t.com", "Pat", "member")
	sam, _ := db.CreateMember("sam@t.com", "Sam", "member")

	tok, err := db.CalendarToken(pat.ID)
	if err != nil || len(tok) < 32 {
		t.Fatalf("CalendarToken = %q %v", tok, err)
	}
	if again, _ := db.CalendarToken(pat.ID); again != tok {
		t.Fatal("asking again should show the same token")
	}
	if other, _ := db.CalendarToken(sam.ID); other == tok {
		t.Fatal("two accounts, two tokens")
	}
	u, err := db.UserByCalendarToken(tok)
	if err != nil || u.ID != pat.ID {
		t.Fatalf("UserByCalendarToken = %v %v", u, err)
	}
	if _, err := db.UserByCalendarToken("nope"); err == nil {
		t.Fatal("an unknown token must not resolve")
	}
	fresh, err := db.ResetCalendarToken(pat.ID)
	if err != nil || fresh == tok {
		t.Fatalf("ResetCalendarToken = %q %v", fresh, err)
	}
	if _, err := db.UserByCalendarToken(tok); err == nil {
		t.Fatal("the old token must stop working")
	}
	if u, _ := db.UserByCalendarToken(fresh); u == nil || u.ID != pat.ID {
		t.Fatal("the new token resolves")
	}
	if _, err := db.CalendarToken(""); err == nil {
		t.Fatal("no token for nobody")
	}

	// RSVPEventIDsForUser: going/maybe/invited count, not_going doesn't, by profile.
	patA := db.DefaultAuthorID(pat.ID)
	if got := db.RSVPEventIDsForUser(pat.ID); len(got) != 0 {
		t.Fatalf("no answers yet: %v", got)
	}
	for i, answer := range []string{"going", "maybe", "not_going"} {
		post, _ := db.CreateRecord("events", "e"+string(rune('a'+i)), "E", "", "published", patA)
		ev, _, _ := ParseWhen(map[string]any{"when": "2027-05-0" + string(rune('1'+i)) + " 19:00"}, nil)
		db.ReconcileWhen(post, ev)
		ev = db.EventFor(post)
		key, _ := ev.ResolveOccurrence("", ev.Starts.AddDate(0, -1, 0))
		db.SetRSVP(ev.ID, key, patA, answer)
	}
	inv, _ := db.CreateRecord("events", "inv", "Inv", "", "published", patA)
	iev, _, _ := ParseWhen(map[string]any{"when": "2027-06-01 19:00"}, nil)
	db.ReconcileWhen(inv, iev)
	iev = db.EventFor(inv)
	ikey, _ := iev.ResolveOccurrence("", iev.Starts.AddDate(0, -1, 0))
	db.InviteRSVP(iev.ID, ikey, patA)
	got := db.RSVPEventIDsForUser(pat.ID)
	if len(got) != 3 || !got[iev.ID] {
		t.Fatalf("RSVPEventIDsForUser = %v (want going, maybe, invited)", got)
	}
}
