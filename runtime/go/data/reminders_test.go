package data

import (
	"errors"
	"testing"
	"time"
)

func TestSendDueReminders(t *testing.T) {
	db, ev, author := rsvpSite(t)
	near := "2026-10-13T19:00:00-07:00" // a Tuesday the weekly club meets
	far := "2026-10-27T19:00:00-07:00"
	for _, occ := range []string{near, far} {
		if err := db.SetRSVP(ev.ID, occ, author, "going"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetRSVPReminder(ev.ID, near, author, "ada@test.com", "https://x.test/events/club"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRSVPReminder(ev.ID, far, author, "ada@test.com", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRSVPReminder(ev.ID, near, author, "nope", ""); err == nil {
		t.Fatal("a bad address should be refused")
	}
	if err := db.SetRSVPReminder(ev.ID, "2026-10-20T19:00:00-07:00", author, "ada@test.com", ""); err == nil {
		t.Fatal("a reminder needs an answer to sit on")
	}

	var sent []Reminder
	send := func(r Reminder) error { sent = append(sent, r); return nil }
	loc := ev.Location()

	// Two days out: nothing is due yet.
	if n, err := db.SendDueReminders(time.Date(2026, 10, 11, 19, 0, 0, 0, loc), send); err != nil || n != 0 {
		t.Fatalf("early: sent %d, %v", n, err)
	}
	// Inside the day before: the near one goes, with the page and a readable time.
	n, err := db.SendDueReminders(time.Date(2026, 10, 12, 20, 0, 0, 0, loc), send)
	if err != nil || n != 1 || len(sent) != 1 {
		t.Fatalf("due: sent %d, %v (%v)", n, err, sent)
	}
	if r := sent[0]; r.Email != "ada@test.com" || r.Title != "Club" || r.Link != "https://x.test/events/club" || r.When != "Tue Oct 13, 7 pm – 8:30 pm" {
		t.Fatalf("reminder = %+v", r)
	}
	// Once sent, it isn't sent again.
	if n, _ := db.SendDueReminders(time.Date(2026, 10, 13, 10, 0, 0, 0, loc), send); n != 0 {
		t.Fatalf("resent %d", n)
	}
	// A delivery failure leaves it for next time; one whose date has passed is dropped.
	fail := func(Reminder) error { return errors.New("provider down") }
	if n, _ := db.SendDueReminders(time.Date(2026, 10, 27, 10, 0, 0, 0, loc), fail); n != 0 {
		t.Fatalf("failed send counted %d", n)
	}
	if n, _ := db.SendDueReminders(time.Date(2026, 10, 27, 12, 0, 0, 0, loc), send); n != 1 || len(sent) != 2 {
		t.Fatalf("retry: sent %d", n)
	}
	if err := db.SetRSVP(ev.ID, "2026-11-03T19:00:00-07:00", author, "going"); err != nil {
		t.Fatal(err)
	}
	db.SetRSVPReminder(ev.ID, "2026-11-03T19:00:00-07:00", author, "ada@test.com", "")
	if n, _ := db.SendDueReminders(time.Date(2026, 11, 5, 0, 0, 0, 0, loc), send); n != 0 || len(sent) != 2 {
		t.Fatalf("a past date should be missed, sent %d", n)
	}
	var status string
	db.Conn.QueryRow(`SELECT reminder_sent FROM rsvps WHERE event_id = ? AND occurrence = ?`, ev.ID, "2026-11-03T19:00:00-07:00").Scan(&status)
	if status != "missed" {
		t.Fatalf("past reminder status = %q", status)
	}
	// Someone who can't go gets no reminder.
	db.SetRSVP(ev.ID, "2026-11-10T19:00:00-08:00", author, "going")
	db.SetRSVPReminder(ev.ID, "2026-11-10T19:00:00-08:00", author, "ada@test.com", "")
	db.SetRSVP(ev.ID, "2026-11-10T19:00:00-08:00", author, "not_going")
	if n, _ := db.SendDueReminders(time.Date(2026, 11, 10, 10, 0, 0, 0, loc), send); n != 0 {
		t.Fatalf("not going was reminded")
	}
}
