package data

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// RSVPs — "are you coming?" on an event, per occurrence.
//
// A member answers going / not_going / maybe for one instance of a series (a
// one-off's only instance is itself). Counts are public; who answered is for
// the post's author and moderators. A series edit re-keys answers whose date
// moved (see RekeyRSVPs); answers to a date that no longer exists are kept and
// shown to the organizer as no longer scheduled.

// RSVPAnswers are the accepted answers.
var RSVPAnswers = map[string]bool{"going": true, "not_going": true, "maybe": true}

// ValidRSVPAnswer reports whether s is one of the three answers.
func ValidRSVPAnswer(s string) bool { return RSVPAnswers[s] }

// RSVPCounts is the public tally for one occurrence.
type RSVPCounts struct {
	Going    int `json:"going"`
	NotGoing int `json:"not_going"`
	Maybe    int `json:"maybe"`
}

// ResolveOccurrence validates a requested occurrence against a series: "" means
// the next occurrence; otherwise the time must be a real instance (within two
// years, skipped dates excluded). Returns the canonical key — the occurrence's
// start in the event's zone as RFC 3339 — or an error a person can read.
func (e *Event) ResolveOccurrence(requested string, now time.Time) (string, error) {
	if requested == "" {
		n := e.Next(now)
		if n == nil {
			return "", fmt.Errorf("this event has no upcoming date")
		}
		return n.Starts.Format(time.RFC3339), nil
	}
	want, err := time.Parse(time.RFC3339, requested)
	if err != nil {
		return "", fmt.Errorf("occurrence must be an RFC 3339 time, got %q", requested)
	}
	for _, occ := range e.Occurrences(want.Add(-time.Second), want.Add(time.Second), 2) {
		if occ.Starts.Equal(want) {
			return occ.Starts.Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("that isn't a date this event happens on")
}

// SetRSVP records or changes a member's answer.
func (db *DB) SetRSVP(eventID, occurrence, authorID, answer string) error {
	if !ValidRSVPAnswer(answer) {
		return fmt.Errorf("answer must be going, not_going, or maybe")
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO rsvps (id, site_id, event_id, occurrence, author_id, answer, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(event_id, occurrence, author_id) DO UPDATE SET answer=excluded.answer, updated=excluded.updated`,
		GenerateID(), db.SiteID, eventID, occurrence, authorID, answer, now, now,
	)
	return err
}

// DeleteRSVP withdraws a member's answer (no error when there was none).
func (db *DB) DeleteRSVP(eventID, occurrence, authorID string) error {
	_, err := db.Conn.Exec(
		`DELETE FROM rsvps WHERE site_id = ? AND event_id = ? AND occurrence = ? AND author_id = ?`,
		db.SiteID, eventID, occurrence, authorID,
	)
	return err
}

// RSVPCountsFor tallies one occurrence.
func (db *DB) RSVPCountsFor(eventID, occurrence string) (RSVPCounts, error) {
	rows, err := db.Conn.Query(
		`SELECT answer, COUNT(*) FROM rsvps WHERE site_id = ? AND event_id = ? AND occurrence = ? GROUP BY answer`,
		db.SiteID, eventID, occurrence,
	)
	if err != nil {
		return RSVPCounts{}, err
	}
	defer rows.Close()
	var c RSVPCounts
	for rows.Next() {
		var answer string
		var n int
		if err := rows.Scan(&answer, &n); err != nil {
			return c, err
		}
		switch answer {
		case "going":
			c.Going = n
		case "not_going":
			c.NotGoing = n
		case "maybe":
			c.Maybe = n
		}
	}
	return c, rows.Err()
}

// RSVPForUser is the account's own answer for an occurrence ("" if none). An
// account may hold several personas; any of them counts as "mine".
func (db *DB) RSVPForUser(eventID, occurrence, userID string) string {
	var answer string
	err := db.Conn.QueryRow(
		`SELECT r.answer FROM rsvps r JOIN authors a ON a.id = r.author_id
		 WHERE r.site_id = ? AND r.event_id = ? AND r.occurrence = ? AND a.user_id = ? LIMIT 1`,
		db.SiteID, eventID, occurrence, userID,
	).Scan(&answer)
	if err != nil {
		return ""
	}
	return answer
}

// ListRSVPs lists who answered for one occurrence (organizer view), with each
// author's display name and an email to reach them (the persona's, else the
// account's — a pen name still has a person behind it).
func (db *DB) ListRSVPs(eventID, occurrence string) ([]map[string]any, error) {
	rows, err := db.Conn.Query(
		`SELECT r.id, r.occurrence, r.author_id, r.answer, r.created, r.updated,
		        COALESCE(a.name, ''), COALESCE(NULLIF(a.email, ''), u.email, '')
		 FROM rsvps r LEFT JOIN authors a ON a.id = r.author_id
		               LEFT JOIN users u ON u.id = a.user_id
		 WHERE r.site_id = ? AND r.event_id = ? AND (? = '' OR r.occurrence = ?)
		 ORDER BY r.occurrence, r.answer, r.created`,
		db.SiteID, eventID, occurrence, occurrence,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, occ, authorID, answer, created, updated, name, email string
		if err := rows.Scan(&id, &occ, &authorID, &answer, &created, &updated, &name, &email); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "occurrence": occ, "author_id": authorID, "answer": answer,
			"author_name": name, "author_email": email, "created": created, "updated": updated,
		})
	}
	return out, rows.Err()
}

// RSVPOccurrences lists the distinct occurrences that have answers, oldest first.
func (db *DB) RSVPOccurrences(eventID string) ([]string, error) {
	rows, err := db.Conn.Query(
		`SELECT DISTINCT occurrence FROM rsvps WHERE site_id = ? AND event_id = ? ORDER BY occurrence`,
		db.SiteID, eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RekeyRSVPs runs after a series is edited: an answer keyed to a start that no
// longer exists moves to the new occurrence on the same calendar day, if there
// is one (the common "moved 7pm to 8pm" case). Anything else stays as it is.
func (db *DB) RekeyRSVPs(e *Event) error {
	keys, err := db.RSVPOccurrences(e.ID)
	if err != nil || len(keys) == 0 {
		return err
	}
	loc := e.Location()
	for _, key := range keys {
		old, err := time.Parse(time.RFC3339, key)
		if err != nil {
			continue
		}
		old = old.In(loc)
		day := time.Date(old.Year(), old.Month(), old.Day(), 0, 0, 0, 0, loc)
		occs := e.Occurrences(day, day.AddDate(0, 0, 1), 0)
		if len(occs) == 0 {
			continue // no longer scheduled that day; the organizer sees it
		}
		fresh := occs[0].Starts.Format(time.RFC3339)
		if fresh == key {
			continue
		}
		// Move, unless the member already answered for the new key (keep theirs).
		db.Conn.Exec(
			`UPDATE OR IGNORE rsvps SET occurrence = ?, updated = ? WHERE site_id = ? AND event_id = ? AND occurrence = ?`,
			fresh, time.Now().UTC().Format("2006-01-02T15:04:05Z"), db.SiteID, e.ID, key,
		)
		db.Conn.Exec(`DELETE FROM rsvps WHERE site_id = ? AND event_id = ? AND occurrence = ?`, db.SiteID, e.ID, key)
	}
	return nil
}

// DeleteRSVPsForEvent removes every answer on a series (a deleted post).
func (db *DB) DeleteRSVPsForEvent(eventID string) error {
	_, err := db.Conn.Exec(`DELETE FROM rsvps WHERE site_id = ? AND event_id = ?`, db.SiteID, eventID)
	return err
}

// RSVPSummary is what a template sees as record.rsvps: the tally for the next
// occurrence, plus which occurrence that is.
func (db *DB) RSVPSummary(e *Event, now time.Time) map[string]any {
	if e == nil {
		return nil
	}
	key, err := e.ResolveOccurrence("", now)
	if err != nil {
		return map[string]any{"going": 0, "not_going": 0, "maybe": 0, "occurrence": ""}
	}
	c, _ := db.RSVPCountsFor(e.ID, key)
	return map[string]any{"going": c.Going, "not_going": c.NotGoing, "maybe": c.Maybe, "occurrence": key}
}

// AttendeeRows flattens every answer on a series for the organizer, grouped by
// occurrence (soonest first) and flagging occurrences the series no longer has.
func (db *DB) AttendeeRows(e *Event) ([]map[string]any, error) {
	list, err := db.ListRSVPs(e.ID, "")
	if err != nil {
		return nil, err
	}
	loc := e.Location()
	scheduled := map[string]bool{}
	for _, key := range uniqueOccurrences(list) {
		t, err := time.Parse(time.RFC3339, key)
		if err != nil {
			continue
		}
		for _, occ := range e.Occurrences(t.Add(-time.Second), t.Add(time.Second), 2) {
			if occ.Starts.Equal(t) {
				scheduled[key] = true
			}
		}
	}
	for _, r := range list {
		key, _ := r["occurrence"].(string)
		r["scheduled"] = scheduled[key]
		if t, err := time.Parse(time.RFC3339, key); err == nil {
			r["occurrence_text"] = FormatWhen(t.In(loc), time.Time{}, e.AllDay, time.Now().In(loc), "")
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		oi, _ := list[i]["occurrence"].(string)
		oj, _ := list[j]["occurrence"].(string)
		return oi < oj
	})
	return list, nil
}

func uniqueOccurrences(list []map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range list {
		key, _ := r["occurrence"].(string)
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// UserOwnsEventPost reports whether the account authored the post a series is on.
func (db *DB) UserOwnsEventPost(userID string, e *Event) bool {
	if e == nil || e.TargetType != "post" {
		return false
	}
	return db.UserOwnsPost(userID, e.TargetID)
}

var _ = sql.ErrNoRows
