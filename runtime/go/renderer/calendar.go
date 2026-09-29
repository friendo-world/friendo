package renderer

import (
	"fmt"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"

	"github.com/friendo-world/friendo/runtime/go/calendar"
	"github.com/friendo-world/friendo/runtime/go/data"
)

// Calendar filters.
//
//	{% for e in collections.events|upcoming %}      occurrences from now, soonest first
//	{% for e in collections.events|upcoming:5 %}    …at most five
//	{% for e in collections.events|past %}          occurrences before now, most recent first
//	{% for e in collections.events|in_month:"2026-10" %}
//	{% for e in collections.events|on_day:"2026-10-04" %}
//	{{ record.when|when }}                          "Sat Oct 4, 10 am – 4 pm"
//	{{ record.when|when:"2 January" }}              …with a different date layout
//
// The four list filters *expand* recurring series: a weekly meet-up yields one
// entry per week, each a shallow copy of the record whose `when` is that
// occurrence (when.occurrence = true), so a listing loops over occurrences while
// {{ e.slug }} still links to the one post. Every record carries `when` already
// (attached in the page context), so these need no database access.

// upcomingWindow is how far ahead `upcoming` looks by default.
const upcomingWindow = 12 // months

func init() {
	pongo2.RegisterFilter("google_calendar_url", filterGoogleCalendarURL)
	pongo2.RegisterFilter("upcoming", filterUpcoming)
	pongo2.RegisterFilter("past", filterPast)
	pongo2.RegisterFilter("in_month", filterInMonth)
	pongo2.RegisterFilter("on_day", filterOnDay)
	pongo2.RegisterFilter("when", filterWhen)
}

// Now is the clock the calendar filters use; tests pin it.
var Now = time.Now

// whenOf reads a record's when map, whichever way it was stored.
func whenOf(v any) data.When {
	switch w := v.(type) {
	case data.When:
		return w
	case map[string]any:
		return data.When(w)
	}
	return nil
}

// seriesOf pulls the *data.Event out of a record's when map.
func seriesOf(record map[string]any) *data.Event {
	when := whenOf(record["when"])
	if when == nil {
		return nil
	}
	ev, _ := when["_series"].(*data.Event)
	return ev
}

// expand turns a list of records into a list of occurrence-records within
// [from, to), soonest first.
func expand(in *pongo2.Value, from, to time.Time, limit int) []map[string]any {
	records, ok := in.Interface().([]map[string]any)
	if !ok {
		return nil
	}
	type hit struct {
		occ data.Occurrence
		rec map[string]any
	}
	var hits []hit
	for _, rec := range records {
		ev := seriesOf(rec)
		if ev == nil {
			continue
		}
		for _, occ := range ev.Occurrences(from, to, 0) {
			hits = append(hits, hit{occ, rec})
		}
	}
	// Stable sort by start.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].occ.Starts.Before(hits[j-1].occ.Starts); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		copy := make(map[string]any, len(h.rec))
		for k, v := range h.rec {
			copy[k] = v
		}
		copy["when"] = h.occ.Map()
		out = append(out, copy)
	}
	return out
}

func filterUpcoming(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	now := Now()
	limit := 0
	if param != nil && !param.IsNil() {
		limit = param.Integer()
	}
	return pongo2.AsValue(expand(in, now, now.AddDate(0, upcomingWindow, 0), limit)), nil
}

func filterPast(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	now := Now()
	limit := 0
	if param != nil && !param.IsNil() {
		limit = param.Integer()
	}
	list := expand(in, now.AddDate(0, -upcomingWindow, 0), now, 0)
	// Only what has already started, most recent first.
	var out []map[string]any
	for i := len(list) - 1; i >= 0; i-- {
		when := whenOf(list[i]["when"])
		if s, _ := when["start"].(string); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil && !t.Before(now) {
				continue
			}
		}
		out = append(out, list[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return pongo2.AsValue(out), nil
}

func filterInMonth(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	spec := strings.TrimSpace(param.String())
	loc := Now().Location()
	if records, ok := in.Interface().([]map[string]any); ok {
		loc = firstLocation(records, loc)
	}
	var from time.Time
	if spec == "" {
		n := Now().In(loc)
		from = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	} else {
		t, err := time.ParseInLocation("2006-01", spec, loc)
		if err != nil {
			return nil, &pongo2.Error{Sender: "filter:in_month", OrigError: fmt.Errorf("want YYYY-MM, got %q", spec)}
		}
		from = t
	}
	return pongo2.AsValue(expand(in, from, from.AddDate(0, 1, 0), 0)), nil
}

func filterOnDay(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	spec := strings.TrimSpace(param.String())
	loc := Now().Location()
	if records, ok := in.Interface().([]map[string]any); ok {
		loc = firstLocation(records, loc)
	}
	var from time.Time
	if spec == "" {
		n := Now().In(loc)
		from = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	} else {
		t, err := time.ParseInLocation("2006-01-02", spec, loc)
		if err != nil {
			return nil, &pongo2.Error{Sender: "filter:on_day", OrigError: fmt.Errorf("want YYYY-MM-DD, got %q", spec)}
		}
		from = t
	}
	return pongo2.AsValue(expand(in, from, from.AddDate(0, 0, 1), 0)), nil
}

// firstLocation is the zone of the first event in a list — month and day
// boundaries are read in the events' own zone, which on one site is the site's.
func firstLocation(records []map[string]any, fallback *time.Location) *time.Location {
	for _, r := range records {
		if ev := seriesOf(r); ev != nil {
			return ev.Location()
		}
	}
	return fallback
}

// filterWhen formats a when map (post.when, one date of it, or post.when.next)
// for people, with a Go date layout for the date part. {{ post.when }} alone
// prints the same thing with the default layout.
func filterWhen(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	when := whenOf(in.Interface())
	if when == nil {
		return pongo2.AsValue(""), nil
	}
	starts, _ := when["start"].(string)
	ends, _ := when["end"].(string)
	allDay, _ := when["all_day"].(bool)
	st, err := time.Parse(time.RFC3339, starts)
	if err != nil {
		return pongo2.AsValue(""), nil
	}
	var en time.Time
	if ends != "" {
		en, _ = time.Parse(time.RFC3339, ends)
	}
	layout := ""
	if param != nil && !param.IsNil() {
		layout = param.String()
	}
	return pongo2.AsValue(data.FormatWhen(st, en, allDay, Now().In(st.Location()), layout)), nil
}

// filterGoogleCalendarURL turns a post (with `when`, and `location` if it has one)
// into Google Calendar's pre-filled "add event" link: {{ post|google_calendar_url }}.
// An expanded date (from upcoming/in_month/…) links that one date; a series
// links with its rule so Google repeats it. "" for a post with no time.
func filterGoogleCalendarURL(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	record, ok := in.Interface().(map[string]any)
	if !ok || record == nil {
		return pongo2.AsValue(""), nil
	}
	when := whenOf(record["when"])
	if when == nil {
		return pongo2.AsValue(""), nil
	}
	startsStr, _ := when["start"].(string)
	starts, err := time.Parse(time.RFC3339, startsStr)
	if err != nil {
		return pongo2.AsValue(""), nil
	}
	var ends time.Time
	if e, _ := when["end"].(string); e != "" {
		ends, _ = time.Parse(time.RFC3339, e)
	}
	if ev := seriesOf(record); ev != nil {
		// Re-express in the named zone so Google gets the right ctz.
		starts = starts.In(ev.Location())
		if !ends.IsZero() {
			ends = ends.In(ev.Location())
		}
	}
	allDay, _ := when["all_day"].(bool)
	rule := ""
	if occ, _ := when["date"].(bool); !occ {
		rule, _ = when["rule"].(string)
	}
	title, _ := record["title"].(string)
	body, _ := record["body"].(string)
	place := ""
	if loc, _ := record["location"].(map[string]any); loc != nil {
		place, _ = loc["label"].(string)
	}
	return pongo2.AsValue(calendar.GoogleEventURL(title, starts, ends, allDay, rule, calendar.PlainText(body), place)), nil
}
