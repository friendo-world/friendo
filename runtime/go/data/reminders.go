package data

import (
	"fmt"
	"strings"
	"time"
)

// Reminders — "email me the day before" on an RSVP.
//
// Anyone who answers going or maybe may leave an email to be reminded at;
// a visitor has no account, so it's the only way they can be. The email lives
// on the RSVP row (reminder_email) and nowhere else; the organizer sees it
// beside the answer (ListRSVPs), nobody else does, and it doesn't make the
// visitor a member. The reminder goes out once,
// in the day before the occurrence (SendDueReminders, called on a timer by the
// server), and reminder_sent records that it did.

// ReminderWindow is how far ahead of an occurrence its reminder is sent.
const ReminderWindow = 24 * time.Hour

// maxReminderEmail bounds what's accepted as an address.
const maxReminderEmail = 254

// ValidReminderEmail is the light check an address gets before it's kept.
func ValidReminderEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if len(email) > maxReminderEmail {
		return fmt.Errorf("that email is too long")
	}
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 || !strings.Contains(email[at+1:], ".") || strings.ContainsAny(email, " \t\r\n") {
		return fmt.Errorf("that doesn't look like an email address")
	}
	return nil
}

// SetRSVPReminder asks for a reminder on an existing answer; link is the
// event page's full address, kept for the email. An empty email clears it.
func (db *DB) SetRSVPReminder(eventID, occurrence, authorID, email, link string) error {
	email = NormalizeEmail(email)
	if email != "" {
		if err := ValidReminderEmail(email); err != nil {
			return err
		}
	} else {
		link = ""
	}
	res, err := db.Conn.Exec(
		`UPDATE rsvps SET reminder_email = ?, reminder_link = ?, reminder_sent = '', updated = ?
		 WHERE site_id = ? AND event_id = ? AND occurrence = ? AND author_id = ?`,
		email, link, time.Now().UTC().Format("2006-01-02T15:04:05Z"), db.SiteID, eventID, occurrence, authorID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("answer first, then ask for a reminder")
	}
	return nil
}

// RSVPReminderFor is the email an account asked to be reminded at for an
// occurrence ("" if none), across any of its profiles.
func (db *DB) RSVPReminderFor(eventID, occurrence, userID string) string {
	var email string
	db.Conn.QueryRow(
		`SELECT r.reminder_email FROM rsvps r JOIN authors a ON a.id = r.author_id
		 WHERE r.site_id = ? AND r.event_id = ? AND r.occurrence = ? AND a.user_id = ? AND r.reminder_email != '' LIMIT 1`,
		db.SiteID, eventID, occurrence, userID,
	).Scan(&email)
	return email
}

// Reminder is one email to send: who, about what, when, and where.
type Reminder struct {
	ID    string // the RSVP row
	Email string
	Title string
	When  string // the occurrence, as a person reads it
	Link  string
}

// SendDueReminders emails every reminder whose occurrence starts within
// ReminderWindow of now and marks it sent; one whose date has already passed
// is marked missed instead. send delivers one email; a failure leaves the row
// to try again next time. It reports how many were sent.
func (db *DB) SendDueReminders(now time.Time, send func(Reminder) error) (int, error) {
	rows, err := db.Conn.Query(
		`SELECT r.id, r.event_id, r.occurrence, r.reminder_email, r.reminder_link
		 FROM rsvps r WHERE r.site_id = ? AND r.reminder_email != '' AND r.reminder_sent = ''
		   AND r.answer IN ('going', 'maybe')`,
		db.SiteID,
	)
	if err != nil {
		return 0, err
	}
	type pending struct{ id, eventID, occurrence, email, link string }
	var due []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.eventID, &p.occurrence, &p.email, &p.link); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	stamp := now.UTC().Format("2006-01-02T15:04:05Z")
	mark := func(id, status string) {
		db.Conn.Exec(`UPDATE rsvps SET reminder_sent = ? WHERE id = ?`, status, id)
	}
	sent := 0
	for _, p := range due {
		starts, err := time.Parse(time.RFC3339, p.occurrence)
		if err != nil {
			mark(p.id, "missed")
			continue
		}
		if !starts.After(now) {
			mark(p.id, "missed")
			continue
		}
		if starts.Sub(now) > ReminderWindow {
			continue // not yet
		}
		ev, err := db.GetEvent(p.eventID)
		if err != nil || ev == nil {
			mark(p.id, "missed")
			continue
		}
		rec, err := db.GetRecordByID(ev.TargetID)
		if err != nil {
			mark(p.id, "missed")
			continue
		}
		if st, _ := rec["status"].(string); st != "published" {
			mark(p.id, "missed")
			continue
		}
		title, _ := rec["title"].(string)
		if title == "" {
			title = "Your event"
		}
		loc := ev.Location()
		var ends time.Time
		for _, occ := range ev.Occurrences(starts.Add(-time.Second), starts.Add(time.Second), 2) {
			if occ.Starts.Equal(starts) {
				ends = occ.Ends
			}
		}
		r := Reminder{
			ID:    p.id,
			Email: p.email,
			Title: title,
			When:  FormatWhen(starts.In(loc), ends, ev.AllDay, now.In(loc), ""),
			Link:  p.link,
		}
		if err := send(r); err != nil {
			continue // try again next time
		}
		mark(p.id, stamp)
		sent++
	}
	return sent, nil
}
