package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/friendo-world/friendo/runtime/go/content"
	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/renderer"
	"github.com/friendo-world/friendo/runtime/go/server"
)

// The calendar end to end, the way a site author meets it: events written as
// content files with `when`, listed with the calendar filters, shown on the
// event page as record.when, subscribed to at /calendar.ics, read by the SDK at
// /calendar.json — with a members-only collection kept out of both feeds and a
// page at /calendar.ics taking the feed's place.
func TestCalendarEndToEnd(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	// The dates are relative so the test outlives the calendar: the fair is the
	// first Sunday on or after Oct 2 of next year (in the future, inside the two
	// years record.when.next scans, and never spilling a fifth weekly occurrence
	// into October); the club meets two days later, on Tuesdays. The filters'
	// clock is pinned to that Oct 1 so "upcoming" is deterministic; record.when.next
	// reads the real clock.
	year := time.Now().Year() + 1
	fair := time.Date(year, 10, 2, 0, 0, 0, 0, loc)
	for fair.Weekday() != time.Sunday {
		fair = fair.AddDate(0, 0, 1)
	}
	day := func(offset int) time.Time { return fair.AddDate(0, 0, offset) }
	ymd := func(t time.Time) string { return t.Format("2006-01-02") }   // 2027-10-03
	md := func(t time.Time) string { return t.Format("Mon Jan 2") }     // Sun Oct 3
	compact := func(t time.Time) string { return t.Format("20060102") } // 20271003
	renderer.Now = func() time.Time { return time.Date(year, 10, 1, 9, 0, 0, 0, loc) }
	defer func() { renderer.Now = time.Now }()

	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"Village\"\ntimezone = \"America/Los_Angeles\"\n\n[access]\nmembers_only = [\"/private/*\"]\n")
	write("content/events/harvest-fair.md", "---\ntitle: Harvest Fair\nwhen: "+ymd(day(0))+" 10:00 to 16:00\nlocation: 47.6062, -122.3321\nmood: festive\n---\nBring a dish.")
	write("content/events/book-club.md", "---\ntitle: Book club\nwhen:\n  start: "+ymd(day(2))+" 19:00 to 20:30\n  repeats: weekly\n  except: ["+ymd(day(16))+"]\n---\nEvery Tuesday.")
	write("content/events/last-year.md", "---\ntitle: Last year\nwhen: "+ymd(day(0).AddDate(-1, 0, 0))+"\n---\nGone.")
	write("content/events/no-date.md", "---\ntitle: Not an event\n---\nJust a post in the events folder.")
	write("content/events/broken.md", "---\ntitle: Broken\nwhen: next tuesday\n---\nBad date.")
	write("content/private/board.md", "---\ntitle: Board meeting\nwhen: "+ymd(day(8))+" 18:00\n---\nMembers only.")
	write("pages/events/index.html",
		"{% for e in collections.events|upcoming %}[{{ e.title }}|{{ e.when|when }}|/events/{{ e.slug }}]{% endfor %}"+
			"|month:{% for e in collections.events|in_month:\""+fmt.Sprintf("%d-10", year)+"\" %}{{ e.title }};{% endfor %}"+
			"|day:{% for e in collections.events|on_day:\""+ymd(day(9))+"\" %}{{ e.title }};{% endfor %}"+
			"|past:{% for e in collections.events|past %}{{ e.title }};{% endfor %}"+
			"|limit:{% for e in collections.events|upcoming:2 %}{{ e.title }};{% endfor %}")
	write("pages/events/[slug].html", "{{ post.title }}|{{ post.when|when }}|{{ post.when.repeats }}|next:{{ post.when.next|when }}|{{ post.fields.mood }}|when-in-data:{{ post.fields.when }}|going:{{ post.rsvps.going }}|place:{{ post.location.label }}|g:{{ post|google_calendar_url }}|sub:{{ calendar.google }}")
	write("pages/private/[slug].html", "{{ post.title }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := content.Import(siteDir, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "broken.md") {
		t.Errorf("warnings = %v (want one, about broken.md)", res.Warnings)
	}

	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	get := func(path string, headers ...string) (int, string, http.Header) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b), r.Header
	}

	// Listing: the weekly series expands (the third Tuesday skipped), the past and
	// dateless posts are absent, occurrences are soonest first.
	_, body, _ := get("/events/")
	body = strings.SplitN(body, "\n<script", 2)[0] // drop the dev live-reload tag
	wantList := "[Harvest Fair|" + md(day(0)) + ", 10 am – 4 pm|/events/harvest-fair]" +
		"[Book club|" + md(day(2)) + ", 7 pm – 8:30 pm|/events/book-club]" +
		"[Book club|" + md(day(9)) + ", 7 pm – 8:30 pm|/events/book-club]" +
		"[Book club|" + md(day(23)) + ", 7 pm – 8:30 pm|/events/book-club]"
	if !strings.HasPrefix(body, wantList) {
		t.Errorf("upcoming list:\n got %s\nwant prefix %s", body, wantList)
	}
	if !strings.Contains(body, "|month:Harvest Fair;Book club;Book club;Book club;|") {
		t.Errorf("in_month: %s", body)
	}
	if !strings.Contains(body, "|day:Book club;|") {
		t.Errorf("on_day: %s", body)
	}
	if !strings.Contains(body, "|past:Last year;|") {
		t.Errorf("past: %s", body)
	}
	if !strings.HasSuffix(body, "|limit:Harvest Fair;Book club;") {
		t.Errorf("upcoming:2: %s", body)
	}

	// The event page: record.when, its repeats text, the next occurrence, the
	// rest of the front matter intact, and `when` no longer in data.
	_, body, _ = get("/events/book-club")
	if !strings.HasPrefix(body, "Book club|"+md(day(2))+", 7 pm – 8:30 pm|weekly|next:"+md(day(2))+", 7 pm – 8:30 pm||when-in-data:|going:0") {
		t.Errorf("event page: %s", body)
	}
	_, body, _ = get("/events/harvest-fair")
	if !strings.Contains(body, "|festive|") {
		t.Errorf("other front matter lost: %s", body)
	}
	// The pin, Google's add-event link (with the place and zone), and the
	// subscribe link built from the request's origin.
	for _, want := range []string{
		"|place:Harvest Fair|",
		"|g:https://calendar.google.com/calendar/render?action=TEMPLATE&amp;ctz=America%2FLos_Angeles&amp;dates=" + compact(day(0)) + "T100000%2F" + compact(day(0)) + "T160000&amp;details=Bring+a+dish.&amp;location=Harvest+Fair&amp;text=Harvest+Fair|",
		"|sub:https://calendar.google.com/calendar/r?cid=" + url.QueryEscape("webcal://"+strings.TrimPrefix(srv.URL, "http://")+"/calendar.ics"),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("event page missing %q:\n%s", want, body)
		}
	}
	// A recurring series links with its rule; an expanded occurrence links one date.
	_, body, _ = get("/events/book-club")
	if !strings.Contains(body, "recur=RRULE%3AFREQ%3DWEEKLY") {
		t.Errorf("series google link should repeat: %s", body)
	}

	// The feed: three published events, the gated collection absent.
	status, ics, hdr := get("/calendar.ics")
	if status != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/calendar") {
		t.Fatalf("ics: %d %s", status, hdr.Get("Content-Type"))
	}
	unfolded := strings.ReplaceAll(ics, "\r\n ", "")
	if strings.Count(ics, "BEGIN:VEVENT") != 3 {
		t.Errorf("want 3 VEVENTs (fair, club, last year):\n%s", ics)
	}
	for _, want := range []string{
		"X-WR-CALNAME:Village",
		"DTSTART;TZID=America/Los_Angeles:" + compact(day(2)) + "T190000",
		"RRULE:FREQ=WEEKLY",
		"EXDATE;TZID=America/Los_Angeles:" + compact(day(16)) + "T190000",
		"URL:" + srv.URL + "/events/harvest-fair",
		"LOCATION:Harvest Fair", // the pin's label defaults to the title
		"GEO:47.606200;-122.332100",
	} {
		if !strings.Contains(unfolded, want) {
			t.Errorf("ics missing %q:\n%s", want, unfolded)
		}
	}
	if strings.Contains(ics, "Board meeting") {
		t.Error("members-only collection leaked into the public feed")
	}
	if status, _, _ := get("/calendar.ics", "If-None-Match", hdr.Get("ETag")); status != 304 {
		t.Errorf("ETag revalidation: %d", status)
	}

	// One post, one instance.
	_, ics, _ = get("/calendar.ics?collection=events&post=" + recordID(t, db, "events", "book-club") + "&date=" + ymd(day(9)) + "T19:00:00-07:00")
	if strings.Count(ics, "BEGIN:VEVENT") != 1 || strings.Contains(ics, "RRULE") || !strings.Contains(ics, compact(day(9))+"T190000") {
		t.Errorf("single instance:\n%s", ics)
	}

	// The JSON feed for the SDK.
	_, js, _ := get(fmt.Sprintf("/calendar.json?from=%d-10-01&to=%d-11-01", year, year))
	var payload struct {
		Events []struct {
			Title, Starts, URL string
		} `json:"events"`
	}
	json.Unmarshal([]byte(js), &payload)
	if len(payload.Events) != 4 || payload.Events[0].Title != "Harvest Fair" || payload.Events[0].URL != srv.URL+"/events/harvest-fair" {
		t.Errorf("json: %s", js)
	}

	// A page at the feed's path takes over.
	write("pages/calendar.ics.html", "my own calendar page")
	// (the route table reloads on a templates push; here, rebuild the handler)
	handler2, _ := server.BuildSiteHandler(siteDir, db, false)
	srv2 := httptest.NewServer(handler2)
	defer srv2.Close()
	r, _ := http.Get(srv2.URL + "/calendar.ics")
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.HasPrefix(string(b), "my own calendar page") {
		t.Errorf("page override: %s", b)
	}

	// Removing `when` from the file removes the event on re-import.
	write("content/events/harvest-fair.md", "---\ntitle: Harvest Fair\n---\nNo longer scheduled.")
	content.Import(siteDir, db)
	if db.EventFor(recordID(t, db, "events", "harvest-fair")) != nil {
		t.Error("event should be gone after `when` was removed")
	}
}

func recordID(t *testing.T, db *data.DB, collection, slug string) string {
	t.Helper()
	rec, err := db.QueryCollectionByField(collection, "slug", slug)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := rec["id"].(string)
	return id
}
