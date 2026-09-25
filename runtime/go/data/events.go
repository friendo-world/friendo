package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones resolve on a bare container (single binary, no /usr/share/zoneinfo)

	"github.com/BurntSushi/toml"
	"github.com/teambition/rrule-go"
)

// Calendar events.
//
// An event is a post with a `when`. The post's time lives in one `events` row —
// a *series*: a first start/end, an optional recurrence rule, and skipped dates.
// Occurrences are expanded on read (Occurrences / Next); nothing is materialized.
// Times are wall-clock in the event's zone, so a weekly 7pm stays 7pm across a
// daylight-saving change.

// Event is one series attached to a target (today: a post).
type Event struct {
	ID         string
	TargetType string
	TargetID   string
	Starts     time.Time // first occurrence, in loc
	Ends       time.Time // zero = no end (timed: one hour for feeds; all-day: one day)
	AllDay     bool
	Timezone   string // IANA name; "" = the site's default
	RRule      string // RFC 5545 RRULE body ("FREQ=WEEKLY;…"), "" = one-off
	ExDates    []string
	Created    string
	Updated    string

	loc *time.Location // resolved zone (Timezone, else the site's)
}

// Occurrence is one instance of a series.
type Occurrence struct {
	Starts time.Time
	Ends   time.Time // zero when the series has no end
	AllDay bool
	Event  *Event
}

// Location is the zone the event's times are in.
func (e *Event) Location() *time.Location {
	if e.loc != nil {
		return e.loc
	}
	if e.Timezone != "" {
		if l, err := time.LoadLocation(e.Timezone); err == nil {
			e.loc = l
			return l
		}
	}
	return time.UTC
}

// SetLocation resolves the event's zone: its own Timezone if set, else the given
// site default.
func (e *Event) SetLocation(siteLoc *time.Location) {
	if e.Timezone != "" {
		if l, err := time.LoadLocation(e.Timezone); err == nil {
			e.loc = l
			return
		}
	}
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	e.loc = siteLoc
	e.Starts = inLocation(e.Starts, e.loc)
	if !e.Ends.IsZero() {
		e.Ends = inLocation(e.Ends, e.loc)
	}
}

// inLocation re-expresses t's wall-clock reading in loc. Stored times carry a
// fixed offset; the named zone is what expansion needs.
func inLocation(t time.Time, loc *time.Location) time.Time {
	if t.IsZero() {
		return t
	}
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, m, d, h, mi, s, 0, loc)
}

// Duration is the length of one occurrence. A timed event with no end is an hour
// (what the feed emits); an all-day event with no end is one day.
func (e *Event) Duration() time.Duration {
	if e.AllDay {
		if e.Ends.IsZero() {
			return 24 * time.Hour
		}
		// Ends is the last day (inclusive); the exclusive end is the next midnight.
		return e.Ends.AddDate(0, 0, 1).Sub(e.Starts)
	}
	if e.Ends.IsZero() {
		return time.Hour
	}
	return e.Ends.Sub(e.Starts)
}

// Recurring reports whether the series has a rule.
func (e *Event) Recurring() bool { return e.RRule != "" }

// rule builds the rrule-go rule anchored at the series' first occurrence.
func (e *Event) rule() (*rrule.RRule, error) {
	opt, err := rrule.StrToROptionInLocation(e.RRule, e.Location())
	if err != nil {
		return nil, err
	}
	opt.Dtstart = e.Starts
	return rrule.NewRRule(*opt)
}

// MaxOccurrences caps one series' expansion in any window.
const MaxOccurrences = 1000

// Occurrences expands the series into the instances that overlap [from, to),
// soonest first, at most limit (0 = MaxOccurrences). A one-off yields itself if
// it overlaps the window. Skipped dates (ExDates) are left out.
func (e *Event) Occurrences(from, to time.Time, limit int) []Occurrence {
	if limit <= 0 || limit > MaxOccurrences {
		limit = MaxOccurrences
	}
	dur := e.Duration()
	skip := map[string]bool{}
	for _, d := range e.ExDates {
		skip[d] = true
	}
	loc := e.Location()
	mk := func(start time.Time) Occurrence {
		start = inLocation(start, loc)
		occ := Occurrence{Starts: start, AllDay: e.AllDay, Event: e}
		if !e.Ends.IsZero() {
			if e.AllDay {
				occ.Ends = start.Add(dur).AddDate(0, 0, -1)
			} else {
				occ.Ends = start.Add(dur)
			}
		}
		return occ
	}
	overlaps := func(start time.Time) bool {
		return start.Before(to) && !start.Add(dur).Before(from) && start.Add(dur) != from
	}

	if !e.Recurring() {
		if overlaps(e.Starts) && !skip[e.Starts.In(loc).Format("2006-01-02")] {
			return []Occurrence{mk(e.Starts)}
		}
		return nil
	}
	r, err := e.rule()
	if err != nil {
		return nil
	}
	// Start the scan a little early so an in-progress occurrence is included.
	var out []Occurrence
	for _, t := range r.Between(from.Add(-dur), to, true) {
		if !overlaps(t) {
			continue
		}
		if skip[t.In(loc).Format("2006-01-02")] {
			continue
		}
		out = append(out, mk(t))
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Next is the first occurrence ending at or after now, or nil when the series
// is over. Looks up to two years ahead.
func (e *Event) Next(now time.Time) *Occurrence {
	occs := e.Occurrences(now, now.AddDate(2, 0, 0), 1)
	if len(occs) == 0 {
		return nil
	}
	return &occs[0]
}

// Repeats is a human reading of the rule ("weekly", "every 2 weeks on Tue, Thu
// until Dec 31, 2026"), or "" for a one-off.
func (e *Event) Repeats() string {
	return DescribeRule(e.RRule, e.Location())
}

// Map is the event as templates and the API see it: {{ record.when }}.
func (e *Event) Map(now time.Time) map[string]any {
	m := map[string]any{
		"starts":   e.Starts.Format(time.RFC3339),
		"ends":     "",
		"all_day":  e.AllDay,
		"timezone": e.Location().String(),
		"repeats":  e.Repeats(),
		"rule":     e.RRule,
		"except":   append([]string{}, e.ExDates...),
		"next":     nil,
		"_series":  e,
	}
	if !e.Ends.IsZero() {
		m["ends"] = e.Ends.Format(time.RFC3339)
	}
	if n := e.Next(now); n != nil {
		m["next"] = n.Map()
	}
	return m
}

// JSON is Map for the API: the same fields without the template-only series
// pointer.
func (e *Event) JSON(now time.Time) map[string]any {
	m := e.Map(now)
	delete(m, "_series")
	if next, ok := m["next"].(map[string]any); ok {
		delete(next, "_series")
	}
	return m
}

// Map is the occurrence as templates see it (the shape of record.when after an
// expanding filter, and of record.when.next).
func (o Occurrence) Map() map[string]any {
	m := map[string]any{
		"starts":     o.Starts.Format(time.RFC3339),
		"ends":       "",
		"all_day":    o.AllDay,
		"timezone":   o.Starts.Location().String(),
		"occurrence": true,
		"repeats":    "",
		"rule":       "",
		"_series":    o.Event,
	}
	if !o.Ends.IsZero() {
		m["ends"] = o.Ends.Format(time.RFC3339)
	}
	if o.Event != nil {
		m["repeats"] = o.Event.Repeats()
		m["rule"] = o.Event.RRule
	}
	return m
}

// DeterministicEventID derives the stable id of a post's own `when` row. Same
// post → same id across builds and edits, so content re-import and API updates
// upsert in place.
func DeterministicEventID(recordID string) string {
	sum := sha256.Sum256([]byte(recordID + "\nwhen"))
	return hex.EncodeToString(sum[:])[:24]
}

// --- storage ---

const eventCols = `id, target_type, target_id, starts, ends, all_day, timezone, rrule, exdates, created, updated`

func (db *DB) scanEvents(rows *sql.Rows) ([]*Event, error) {
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var e Event
		var starts, ends, exdates string
		var allDay int
		if err := rows.Scan(&e.ID, &e.TargetType, &e.TargetID, &starts, &ends, &allDay, &e.Timezone, &e.RRule, &exdates, &e.Created, &e.Updated); err != nil {
			return nil, err
		}
		e.AllDay = allDay == 1
		e.Starts, _ = time.Parse(time.RFC3339, starts)
		if ends != "" {
			e.Ends, _ = time.Parse(time.RFC3339, ends)
		}
		json.Unmarshal([]byte(exdates), &e.ExDates)
		e.SetLocation(db.Location)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ListEvents returns every series on the site (expansion needs them all — an old
// weekly series still has upcoming occurrences).
func (db *DB) ListEvents() ([]*Event, error) {
	rows, err := db.Conn.Query(`SELECT `+eventCols+` FROM events WHERE site_id = ? ORDER BY starts`, db.SiteID)
	if err != nil {
		return nil, err
	}
	return db.scanEvents(rows)
}

// EventsByTarget indexes a site's series by target id.
func (db *DB) EventsByTarget() (map[string]*Event, error) {
	all, err := db.ListEvents()
	if err != nil {
		return nil, err
	}
	m := make(map[string]*Event, len(all))
	for _, e := range all {
		if _, dup := m[e.TargetID]; !dup {
			m[e.TargetID] = e
		}
	}
	return m, nil
}

// AttachWhen sets record["when"] on every record (nil for a post with no time)
// with one query — the page context, the API listings, and static export all
// use it so a record looks the same everywhere.
func (db *DB) AttachWhen(records []map[string]any, now time.Time, forAPI bool) {
	byTarget, err := db.EventsByTarget()
	if err != nil {
		return
	}
	for _, r := range records {
		id, _ := r["id"].(string)
		ev := byTarget[id]
		switch {
		case ev == nil:
			r["when"] = nil
		case forAPI:
			r["when"] = ev.JSON(now)
		default:
			r["when"] = ev.Map(now)
		}
	}
}

// AttachLocations sets record["location"] on every record — the post's first
// pin as {lat, lng, label}, or nil — with one query, so a template (or the
// google_calendar_url filter) can name the place.
func (db *DB) AttachLocations(records []map[string]any) {
	rows, err := db.Conn.Query(
		`SELECT target_id, lat, lng, label FROM locations WHERE site_id = ? AND target_type = 'post' ORDER BY created`,
		db.SiteID,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	first := map[string]map[string]any{}
	for rows.Next() {
		var id, label string
		var lat, lng float64
		if rows.Scan(&id, &lat, &lng, &label) != nil {
			continue
		}
		if _, seen := first[id]; !seen {
			first[id] = map[string]any{"lat": lat, "lng": lng, "label": label}
		}
	}
	for _, r := range records {
		id, _ := r["id"].(string)
		if loc := first[id]; loc != nil {
			r["location"] = loc
		} else {
			r["location"] = nil
		}
	}
}

// GetEvent returns one series by id, or sql.ErrNoRows.
func (db *DB) GetEvent(id string) (*Event, error) {
	rows, err := db.Conn.Query(`SELECT `+eventCols+` FROM events WHERE site_id = ? AND id = ?`, db.SiteID, id)
	if err != nil {
		return nil, err
	}
	list, err := db.scanEvents(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return list[0], nil
}

// EventFor returns a post's `when` series, or nil.
func (db *DB) EventFor(recordID string) *Event {
	e, err := db.GetEvent(DeterministicEventID(recordID))
	if err != nil {
		return nil
	}
	return e
}

// UpsertEvent inserts or replaces a series by id.
func (db *DB) UpsertEvent(e *Event) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	created := e.Created
	if created == "" {
		created = now
	}
	ex, _ := json.Marshal(e.ExDates)
	if e.ExDates == nil {
		ex = []byte("[]")
	}
	ends := ""
	if !e.Ends.IsZero() {
		ends = e.Ends.Format(time.RFC3339)
	}
	allDay := 0
	if e.AllDay {
		allDay = 1
	}
	_, err := db.Conn.Exec(
		`INSERT INTO events (id, site_id, target_type, target_id, starts, ends, all_day, timezone, rrule, exdates, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   target_type=excluded.target_type, target_id=excluded.target_id,
		   starts=excluded.starts, ends=excluded.ends, all_day=excluded.all_day,
		   timezone=excluded.timezone, rrule=excluded.rrule, exdates=excluded.exdates,
		   updated=excluded.updated`,
		e.ID, db.SiteID, e.TargetType, e.TargetID, e.Starts.Format(time.RFC3339), ends, allDay,
		e.Timezone, e.RRule, string(ex), created, now,
	)
	return err
}

// DeleteEvent removes a series by id (no error when absent).
func (db *DB) DeleteEvent(id string) error {
	_, err := db.Conn.Exec(`DELETE FROM events WHERE id = ? AND site_id = ?`, id, db.SiteID)
	return err
}

// DeleteEventsForTarget removes every series on a target (a deleted post),
// with their RSVPs.
func (db *DB) DeleteEventsForTarget(targetType, targetID string) error {
	db.Conn.Exec(`DELETE FROM rsvps WHERE site_id = ? AND event_id IN (SELECT id FROM events WHERE site_id = ? AND target_type = ? AND target_id = ?)`,
		db.SiteID, db.SiteID, targetType, targetID)
	_, err := db.Conn.Exec(`DELETE FROM events WHERE site_id = ? AND target_type = ? AND target_id = ?`, db.SiteID, targetType, targetID)
	return err
}

// ReconcileWhen makes a post's `when` row match a parsed spec: upsert when
// present, delete when the post no longer declares a time.
func (db *DB) ReconcileWhen(recordID string, e *Event) error {
	id := DeterministicEventID(recordID)
	if e == nil {
		db.DeleteRSVPsForEvent(id)
		return db.DeleteEvent(id)
	}
	e.ID = id
	e.TargetType = "post"
	e.TargetID = recordID
	if err := db.UpsertEvent(e); err != nil {
		return err
	}
	// Answers keyed to a time that moved follow it to the same day.
	return db.RekeyRSVPs(e)
}

// Row is the series as a sync payload (push/pull data carry events beside records).
func (e *Event) Row() map[string]any {
	ends := ""
	if !e.Ends.IsZero() {
		ends = e.Ends.Format(time.RFC3339)
	}
	ex := e.ExDates
	if ex == nil {
		ex = []string{}
	}
	return map[string]any{
		"id": e.ID, "target_type": e.TargetType, "target_id": e.TargetID,
		"starts": e.Starts.Format(time.RFC3339), "ends": ends, "all_day": e.AllDay,
		"timezone": e.Timezone, "rrule": e.RRule, "exdates": ex,
		"created": e.Created, "updated": e.Updated,
	}
}

// EventFromRow rebuilds a series from a sync payload. Returns nil for a row with
// no id or no start.
func EventFromRow(row map[string]any) *Event {
	str := func(k string) string { v, _ := row[k].(string); return v }
	e := &Event{
		ID: str("id"), TargetType: str("target_type"), TargetID: str("target_id"),
		Timezone: str("timezone"), RRule: str("rrule"), Created: str("created"), Updated: str("updated"),
	}
	if e.ID == "" {
		return nil
	}
	var err error
	if e.Starts, err = time.Parse(time.RFC3339, str("starts")); err != nil {
		return nil
	}
	if s := str("ends"); s != "" {
		e.Ends, _ = time.Parse(time.RFC3339, s)
	}
	switch v := row["all_day"].(type) {
	case bool:
		e.AllDay = v
	case float64:
		e.AllDay = v == 1
	}
	if list, ok := row["exdates"].([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				e.ExDates = append(e.ExDates, s)
			}
		}
	}
	if e.TargetType == "" {
		e.TargetType = "post"
	}
	return e
}

// --- site timezone ---

// LoadSiteLocation reads [site] timezone from a site folder's friendo.toml. An
// unset or unknown zone is UTC (the second case is also reported).
func LoadSiteLocation(siteDir string) (*time.Location, error) {
	var cfg struct {
		Site struct {
			Timezone string `toml:"timezone"`
		} `toml:"site"`
	}
	p := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(p); err != nil {
		return time.UTC, nil
	}
	if _, err := toml.DecodeFile(p, &cfg); err != nil {
		return time.UTC, nil
	}
	tz := strings.TrimSpace(cfg.Site.Timezone)
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC, fmt.Errorf("friendo.toml [site] timezone %q is not a known zone (use an IANA name like America/Los_Angeles)", tz)
	}
	return loc, nil
}

// SortOccurrences orders occurrences soonest first (stable).
func SortOccurrences(occs []Occurrence) {
	sort.SliceStable(occs, func(i, j int) bool { return occs[i].Starts.Before(occs[j].Starts) })
}
