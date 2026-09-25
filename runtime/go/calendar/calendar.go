// Package calendar builds a site's calendar feeds from its posts' `when` rows:
// /calendar.ics (iCalendar, for Google Calendar / Apple Calendar / Outlook
// subscriptions) and /calendar.json (expanded occurrences, for <friendo-calendar>).
// Both are public and published-only, and a collection whose page is
// members-only is left out. The same code writes dist/calendar.ics and
// dist/calendar.json in a static export.
package calendar

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
	"github.com/yuin/goldmark"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Entry is one published post with a time.
type Entry struct {
	Event      *data.Event
	Record     map[string]any
	Collection string
	Slug       string
	Title      string
	Body       string
	Updated    string
	Path       string // public URL path ("" when the collection has no page)
	Place      string // the post's pin label, if any
	Lat, Lng   float64
	HasPlace   bool
}

// Filter narrows a feed.
type Filter struct {
	Collection string // one collection
	RecordID   string // one post
}

// Collect gathers the published posts that have a `when`, soonest first.
// visible says whether a collection may appear (nil = all); permalink resolves a
// post's public path (nil = none).
func Collect(db *data.DB, visible func(collection string) bool, permalink data.PermalinkFunc, f Filter) ([]Entry, error) {
	byTarget, err := db.EventsByTarget()
	if err != nil {
		return nil, err
	}
	names, err := db.ListCollections()
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, name := range names {
		if f.Collection != "" && name != f.Collection {
			continue
		}
		if visible != nil && !visible(name) {
			continue
		}
		records, err := db.QueryPublishedCollection(name)
		if err != nil {
			continue
		}
		for _, r := range records {
			id, _ := r["id"].(string)
			if f.RecordID != "" && id != f.RecordID {
				continue
			}
			ev := byTarget[id]
			if ev == nil {
				continue
			}
			e := Entry{Event: ev, Record: r, Collection: name}
			e.Slug, _ = r["slug"].(string)
			e.Title, _ = r["title"].(string)
			e.Body, _ = r["body"].(string)
			e.Updated, _ = r["updated"].(string)
			if permalink != nil {
				e.Path = permalink(name, map[string]string{"slug": e.Slug})
			}
			if locs, err := db.ListLocations("post", id); err == nil && len(locs) > 0 {
				e.Place, _ = locs[0]["label"].(string)
				e.Lat, _ = locs[0]["lat"].(float64)
				e.Lng, _ = locs[0]["lng"].(float64)
				e.HasPlace = true
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Event.Starts.Before(entries[j].Event.Starts) })
	return entries, nil
}

// ETag is a fingerprint of a set of entries: the same posts with the same
// `updated` stamps produce the same tag, so subscribers polling every few hours
// get a cheap 304 when nothing changed.
func ETag(entries []Entry, extra string) string {
	h := sha256.New()
	io.WriteString(h, extra)
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%s|%s|%s\n", e.Event.ID, e.Updated, e.Event.Updated, e.Path)
	}
	return `"` + hex.EncodeToString(h.Sum(nil))[:32] + `"`
}

// --- JSON ---

// Occurrences expands entries into [from, to), soonest first, as the /calendar.json
// shape: one item per occurrence, each pointing back at its post.
func Occurrences(entries []Entry, from, to time.Time, baseURL string) []map[string]any {
	type hit struct {
		occ data.Occurrence
		e   *Entry
	}
	var hits []hit
	for i := range entries {
		e := &entries[i]
		for _, occ := range e.Event.Occurrences(from, to, 0) {
			hits = append(hits, hit{occ, e})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].occ.Starts.Before(hits[j].occ.Starts) })
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		ends := ""
		if !h.occ.Ends.IsZero() {
			ends = h.occ.Ends.Format(time.RFC3339)
		}
		out = append(out, map[string]any{
			"id":         h.e.Event.ID + "-" + h.occ.Starts.Format("20060102"),
			"series_id":  h.e.Event.ID,
			"record_id":  h.e.Event.TargetID,
			"collection": h.e.Collection,
			"slug":       h.e.Slug,
			"title":      h.e.Title,
			"url":        absolute(baseURL, h.e.Path),
			"starts":     h.occ.Starts.Format(time.RFC3339),
			"ends":       ends,
			"all_day":    h.occ.AllDay,
			"timezone":   h.e.Event.Location().String(),
			"repeats":    h.e.Event.Repeats(),
			"rule":       h.e.Event.RRule,
			"place":      h.e.Place,
			"google":     GoogleEventURL(h.e.Title, h.occ.Starts, h.occ.Ends, h.occ.AllDay, "", absolute(baseURL, h.e.Path), h.e.Place),
			"ics":        absolute(baseURL, "/calendar.ics?record="+url.QueryEscape(h.e.Event.TargetID)+"&occurrence="+url.QueryEscape(h.occ.Starts.Format(time.RFC3339))),
		})
	}
	return out
}

// Links are the ways to subscribe to a site's feed, for {{ calendar.* }} in
// templates: the plain feed, a webcal:// address (Apple Calendar, Outlook), and
// Google Calendar's add-a-calendar page. baseURL is the site's public origin;
// with none (a local build with no [deploy] target) only the relative path is
// known, so the app links are empty.
func Links(baseURL string) map[string]string {
	feed := "/calendar.ics"
	out := map[string]string{"ics": feed, "json": "/calendar.json", "webcal": "", "google": ""}
	if baseURL == "" {
		return out
	}
	abs := strings.TrimSuffix(baseURL, "/") + feed
	out["ics"] = abs
	out["json"] = strings.TrimSuffix(baseURL, "/") + "/calendar.json"
	out["webcal"] = "webcal://" + strings.TrimPrefix(strings.TrimPrefix(abs, "https://"), "http://")
	out["google"] = "https://calendar.google.com/calendar/r?cid=" + url.QueryEscape(abs)
	return out
}

// GoogleEventURL builds Google Calendar's pre-filled "create event" link for
// one occurrence (or the series, when rule is set): title, dates in the event's
// zone, details, place, and recurrence.
func GoogleEventURL(title string, starts, ends time.Time, allDay bool, rule, details, place string) string {
	if starts.IsZero() {
		return ""
	}
	q := url.Values{}
	q.Set("action", "TEMPLATE")
	q.Set("text", title)
	if allDay {
		end := starts.AddDate(0, 0, 1)
		if !ends.IsZero() {
			end = ends.AddDate(0, 0, 1)
		}
		q.Set("dates", starts.Format("20060102")+"/"+end.Format("20060102"))
	} else {
		end := ends
		if end.IsZero() {
			end = starts.Add(time.Hour)
		}
		q.Set("dates", starts.Format("20060102T150405")+"/"+end.In(starts.Location()).Format("20060102T150405"))
		q.Set("ctz", starts.Location().String())
	}
	if details != "" {
		if len(details) > 1000 {
			details = details[:1000] + "…"
		}
		q.Set("details", details)
	}
	if place != "" {
		q.Set("location", place)
	}
	if rule != "" {
		q.Set("recur", "RRULE:"+rule)
	}
	return "https://calendar.google.com/calendar/render?" + q.Encode()
}

// PlainText turns a markdown body into readable text (for feed descriptions
// and calendar links).
func PlainText(markdown string) string { return plainText(markdown) }

func absolute(baseURL, path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(baseURL, "/") + path
}

// --- iCalendar ---

// ICSOptions shape a feed.
type ICSOptions struct {
	SiteName   string
	SiteZone   *time.Location
	BaseURL    string // "https://my-site.friendo.world"; "" leaves URL out
	Occurrence string // RFC 3339 start of one instance: emit that instance alone (an "add to my calendar" link)
}

// WriteICS writes an iCalendar document for the entries.
func WriteICS(w io.Writer, entries []Entry, opt ICSOptions) error {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)); b.WriteString("\r\n") }

	zone := "UTC"
	if opt.SiteZone != nil {
		zone = opt.SiteZone.String()
	}
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//friendo//calendar//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	if opt.SiteName != "" {
		line("X-WR-CALNAME:" + escapeText(opt.SiteName))
	}
	line("X-WR-TIMEZONE:" + zone)

	// One VTIMEZONE per zone a timed event uses.
	zones := map[string]*time.Location{}
	var zoneNames []string
	for _, e := range entries {
		loc := e.Event.Location()
		if e.Event.AllDay || loc == time.UTC || loc.String() == "UTC" {
			continue
		}
		if _, seen := zones[loc.String()]; !seen {
			zones[loc.String()] = loc
			zoneNames = append(zoneNames, loc.String())
		}
	}
	sort.Strings(zoneNames)
	now := time.Now()
	for _, name := range zoneNames {
		for _, l := range vtimezone(zones[name], now.Year()-1, now.Year()+3) {
			line(l)
		}
	}

	for i := range entries {
		for _, l := range vevent(&entries[i], opt) {
			line(l)
		}
	}
	line("END:VCALENDAR")
	_, err := io.WriteString(w, b.String())
	return err
}

// vevent renders one entry: the whole series (RRULE + EXDATE), or one instance
// when opt.Occurrence names it.
func vevent(e *Entry, opt ICSOptions) []string {
	ev := e.Event
	loc := ev.Location()
	starts, ends := ev.Starts, ev.Ends
	uid := ev.ID + "@friendo"
	single := false
	if opt.Occurrence != "" {
		want, err := time.Parse(time.RFC3339, opt.Occurrence)
		if err == nil {
			for _, occ := range ev.Occurrences(want.Add(-time.Minute), want.Add(time.Minute), 0) {
				if occ.Starts.Equal(want) {
					starts, ends, single = occ.Starts, occ.Ends, true
					uid = ev.ID + "-" + occ.Starts.Format("20060102T150405") + "@friendo"
					break
				}
			}
		}
		if !single {
			return nil
		}
	}

	var out []string
	out = append(out, "BEGIN:VEVENT")
	out = append(out, "UID:"+uid)
	stamp := parseStamp(e.Updated, ev.Updated)
	out = append(out, "DTSTAMP:"+stamp.UTC().Format("20060102T150405Z"))
	out = append(out, "LAST-MODIFIED:"+stamp.UTC().Format("20060102T150405Z"))
	out = append(out, fmt.Sprintf("SEQUENCE:%d", stamp.Unix()))

	if ev.AllDay {
		out = append(out, "DTSTART;VALUE=DATE:"+starts.Format("20060102"))
		end := starts.AddDate(0, 0, 1)
		if !ends.IsZero() {
			end = ends.AddDate(0, 0, 1) // stored inclusive; iCalendar's DTEND is exclusive
		}
		out = append(out, "DTEND;VALUE=DATE:"+end.Format("20060102"))
	} else {
		out = append(out, dt("DTSTART", starts, loc))
		end := ends
		if end.IsZero() {
			end = starts.Add(time.Hour)
		}
		out = append(out, dt("DTEND", end, loc))
	}

	if ev.Recurring() && !single {
		out = append(out, "RRULE:"+rruleFor(ev))
		for _, d := range ev.ExDates {
			day, err := time.ParseInLocation("2006-01-02", d, loc)
			if err != nil {
				continue
			}
			if ev.AllDay {
				out = append(out, "EXDATE;VALUE=DATE:"+day.Format("20060102"))
			} else {
				at := time.Date(day.Year(), day.Month(), day.Day(), starts.Hour(), starts.Minute(), starts.Second(), 0, loc)
				out = append(out, dt("EXDATE", at, loc))
			}
		}
	}

	out = append(out, "SUMMARY:"+escapeText(e.Title))
	desc := plainText(e.Body)
	if len(desc) > 2000 {
		desc = desc[:2000] + "…"
	}
	if url := absolute(opt.BaseURL, e.Path); url != "" {
		if desc != "" {
			desc += "\n\n"
		}
		desc += url
		out = append(out, "URL:"+url)
	}
	if desc != "" {
		out = append(out, "DESCRIPTION:"+escapeText(desc))
	}
	if e.HasPlace {
		if e.Place != "" {
			out = append(out, "LOCATION:"+escapeText(e.Place))
		}
		out = append(out, fmt.Sprintf("GEO:%.6f;%.6f", e.Lat, e.Lng))
	}
	if e.Collection != "" {
		out = append(out, "CATEGORIES:"+escapeText(e.Collection))
	}
	out = append(out, "END:VEVENT")
	return out
}

// dt renders a date-time property: UTC as a Z stamp, any other zone with TZID.
func dt(prop string, t time.Time, loc *time.Location) string {
	if loc == time.UTC || loc.String() == "UTC" {
		return prop + ":" + t.UTC().Format("20060102T150405Z")
	}
	return prop + ";TZID=" + loc.String() + ":" + t.In(loc).Format("20060102T150405")
}

// rruleFor is the stored rule, with UNTIL reshaped to a DATE for all-day events
// (RFC 5545: UNTIL takes the same form as DTSTART).
func rruleFor(ev *data.Event) string {
	if !ev.AllDay {
		return ev.RRule
	}
	opt, err := rrule.StrToROptionInLocation(ev.RRule, ev.Location())
	if err != nil || opt.Until.IsZero() {
		return ev.RRule
	}
	var parts []string
	for _, p := range strings.Split(ev.RRule, ";") {
		if strings.HasPrefix(p, "UNTIL=") {
			parts = append(parts, "UNTIL="+opt.Until.In(ev.Location()).Format("20060102"))
		} else {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ";")
}

func parseStamp(candidates ...string) time.Time {
	for _, c := range candidates {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, c); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

// escapeText applies RFC 5545 TEXT escaping.
func escapeText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// fold breaks a content line at 75 octets, continuing with CRLF + space, never
// mid-rune.
func fold(s string) string {
	const max = 75
	if len(s) <= max {
		return s
	}
	var b strings.Builder
	width := 0
	limit := max
	for _, r := range s {
		n := len(string(r))
		if width+n > limit {
			b.WriteString("\r\n ")
			width = 0
			limit = max - 1 // the leading space counts
		}
		b.WriteRune(r)
		width += n
	}
	return b.String()
}

var (
	md      = goldmark.New()
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	blankRe = regexp.MustCompile(`\n{3,}`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
)

// plainText turns a markdown body into readable text for DESCRIPTION.
func plainText(markdown string) string {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return ""
	}
	var buf strings.Builder
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		return markdown
	}
	h := buf.String()
	h = regexp.MustCompile(`(?i)</(li|tr)>\s*`).ReplaceAllString(h, "\n")
	h = regexp.MustCompile(`(?i)</(p|div|h[1-6]|blockquote|pre)>`).ReplaceAllString(h, "\n")
	h = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(h, "\n")
	h = tagRe.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	h = spaceRe.ReplaceAllString(h, " ")
	var lines []string
	for _, l := range strings.Split(h, "\n") {
		lines = append(lines, strings.TrimSpace(l))
	}
	h = strings.Join(lines, "\n")
	h = blankRe.ReplaceAllString(h, "\n\n")
	return strings.TrimSpace(h)
}

// vtimezone renders a VTIMEZONE for loc covering the years [fromYear, toYear]:
// one observance per transition found (no RRULE — explicit onsets are valid and
// need no guessing about a zone's rules). Go doesn't expose transitions, so they
// are found by scanning day by day and bisecting to the minute.
func vtimezone(loc *time.Location, fromYear, toYear int) []string {
	out := []string{"BEGIN:VTIMEZONE", "TZID:" + loc.String()}
	type shift struct {
		at       time.Time // the instant (UTC)
		from, to int       // offsets in seconds
		name     string
		dst      bool
	}
	var shifts []shift
	prev := time.Date(fromYear, 1, 1, 0, 0, 0, 0, time.UTC)
	_, prevOff := prev.In(loc).Zone()
	for day := prev.AddDate(0, 0, 1); day.Year() <= toYear; day = day.AddDate(0, 0, 1) {
		_, off := day.In(loc).Zone()
		if off == prevOff {
			continue
		}
		// Bisect between day-1 and day to the minute.
		lo, hi := day.AddDate(0, 0, -1), day
		for hi.Sub(lo) > time.Minute {
			mid := lo.Add(hi.Sub(lo) / 2).Truncate(time.Minute)
			if _, o := mid.In(loc).Zone(); o == prevOff {
				lo = mid
			} else {
				hi = mid
			}
		}
		name, _ := hi.In(loc).Zone()
		shifts = append(shifts, shift{at: hi, from: prevOff, to: off, name: name, dst: hi.In(loc).IsDST()})
		prevOff = off
	}
	if len(shifts) == 0 {
		name, off := prev.In(loc).Zone()
		out = append(out,
			"BEGIN:STANDARD",
			"DTSTART:19700101T000000",
			"TZOFFSETFROM:"+offset(off),
			"TZOFFSETTO:"+offset(off),
			"TZNAME:"+name,
			"END:STANDARD")
	}
	for _, s := range shifts {
		kind := "STANDARD"
		if s.dst {
			kind = "DAYLIGHT"
		}
		// DTSTART is the wall-clock time of the change in the offset it changes from.
		local := s.at.In(time.FixedZone("", s.from))
		out = append(out,
			"BEGIN:"+kind,
			"DTSTART:"+local.Format("20060102T150405"),
			"TZOFFSETFROM:"+offset(s.from),
			"TZOFFSETTO:"+offset(s.to),
			"TZNAME:"+s.name,
			"END:"+kind)
	}
	out = append(out, "END:VTIMEZONE")
	return out
}

func offset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("%s%02d%02d", sign, seconds/3600, (seconds%3600)/60)
}
