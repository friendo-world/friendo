package data

import (
	"os"
	"testing"
	"time"
)

func rsvpSite(t *testing.T) (*DB, *Event, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(dir+"/friendo.toml", []byte("[site]\nname = \"t\"\ntimezone = \"America/Los_Angeles\"\n"), 0o644)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	u, err := db.CreateUser("a@test.com", "Ada", "", "member")
	if err != nil {
		t.Fatal(err)
	}
	post, _ := db.CreateRecord("events", "club", "Club", "", "published", "")
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-06 19:00 to 20:30", "repeats": "weekly", "except": []any{"2026-10-20"}}, db.Location)
	if err := db.ReconcileWhen(post, ev); err != nil {
		t.Fatal(err)
	}
	return db, db.EventFor(post), db.DefaultAuthorID(u.ID)
}

func TestResolveOccurrence(t *testing.T) {
	db, ev, _ := rsvpSite(t)
	_ = db
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, ev.Location())
	next, err := ev.ResolveOccurrence("", now)
	if err != nil || next != "2026-10-13T19:00:00-07:00" {
		t.Errorf("next = %q, %v", next, err)
	}
	// The same instant in another zone resolves to the canonical key.
	key, err := ev.ResolveOccurrence("2026-10-14T02:00:00Z", now)
	if err != nil || key != "2026-10-13T19:00:00-07:00" {
		t.Errorf("utc form = %q, %v", key, err)
	}
	if _, err := ev.ResolveOccurrence("2026-10-20T19:00:00-07:00", now); err == nil {
		t.Error("a skipped date must not resolve")
	}
	if _, err := ev.ResolveOccurrence("2026-10-14T19:00:00-07:00", now); err == nil {
		t.Error("a Wednesday must not resolve")
	}
	if _, err := ev.ResolveOccurrence("tuesday", now); err == nil {
		t.Error("garbage must not resolve")
	}
}

func TestRSVPLifecycle(t *testing.T) {
	db, ev, author := rsvpSite(t)
	key := "2026-10-13T19:00:00-07:00"
	if err := db.SetRSVP(ev.ID, key, author, "going"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRSVP(ev.ID, key, author, "maybe"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRSVP(ev.ID, key, author, "sure"); err == nil {
		t.Error("bad answer accepted")
	}
	c, _ := db.RSVPCountsFor(ev.ID, key)
	if c.Going != 0 || c.Maybe != 1 {
		t.Errorf("counts after change of mind: %+v", c)
	}
	var userID string
	db.Conn.QueryRow(`SELECT user_id FROM authors WHERE id = ?`, author).Scan(&userID)
	if db.RSVPForUser(ev.ID, key, userID) != "maybe" {
		t.Error("mine should be maybe")
	}
	if db.RSVPForUser(ev.ID, "2026-10-27T19:00:00-07:00", userID) != "" {
		t.Error("another occurrence should have no answer")
	}
	list, _ := db.ListRSVPs(ev.ID, key)
	if len(list) != 1 || list[0]["author_name"] != "Ada" || list[0]["author_email"] != "a@test.com" {
		t.Errorf("organizer list: %+v", list)
	}
	rows, _ := db.AttendeeRows(ev)
	if len(rows) != 1 || rows[0]["scheduled"] != true || rows[0]["occurrence_text"] == "" {
		t.Errorf("attendee rows: %+v", rows)
	}
	sum := db.RSVPSummary(ev, time.Date(2026, 10, 10, 0, 0, 0, 0, ev.Location()))
	if sum["maybe"] != 1 || sum["occurrence"] != key {
		t.Errorf("summary: %+v", sum)
	}
	db.DeleteRSVP(ev.ID, key, author)
	c, _ = db.RSVPCountsFor(ev.ID, key)
	if c.Maybe != 0 {
		t.Error("withdrawn answer still counted")
	}
}

// Moving the series' time re-keys answers to the same day; changing the weekday
// leaves them, flagged as no longer scheduled.
func TestRekeyRSVPs(t *testing.T) {
	db, ev, author := rsvpSite(t)
	db.SetRSVP(ev.ID, "2026-10-13T19:00:00-07:00", author, "going")
	moved, _, _ := ParseWhen(map[string]any{"when": "2026-10-06 20:00 to 21:00", "repeats": "weekly"}, db.Location)
	if err := db.ReconcileWhen(ev.TargetID, moved); err != nil {
		t.Fatal(err)
	}
	keys, _ := db.RSVPOccurrences(ev.ID)
	if len(keys) != 1 || keys[0] != "2026-10-13T20:00:00-07:00" {
		t.Errorf("re-keyed to the new time: %v", keys)
	}
	wed, _, _ := ParseWhen(map[string]any{"when": "2026-10-07 20:00", "repeats": "weekly"}, db.Location)
	db.ReconcileWhen(ev.TargetID, wed)
	rows, _ := db.AttendeeRows(db.EventFor(ev.TargetID))
	if len(rows) != 1 || rows[0]["scheduled"] != false {
		t.Errorf("orphaned answer should remain, unscheduled: %+v", rows)
	}
	// Deleting the post deletes its answers.
	db.DeleteRecord(ev.TargetID)
	if list, _ := db.ListRSVPs(ev.ID, ""); len(list) != 0 {
		t.Error("answers should go with the post")
	}
}
