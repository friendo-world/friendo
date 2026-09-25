package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	// Pin the clock so "upcoming" is deterministic.
	renderer.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, loc) }
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
	write("content/events/harvest-fair.md", "---\ntitle: Harvest Fair\nwhen: 2026-10-04 10:00 to 16:00\nlocation: 47.6062, -122.3321\nmood: festive\n---\nBring a dish.")
	write("content/events/book-club.md", "---\ntitle: Book club\nwhen: 2026-10-06 19:00 to 20:30\nrepeats: weekly\nexcept: [2026-10-20]\n---\nEvery Tuesday.")
	write("content/events/last-year.md", "---\ntitle: Last year\nwhen: 2025-10-04\n---\nGone.")
	write("content/events/no-date.md", "---\ntitle: Not an event\n---\nJust a post in the events folder.")
	write("content/events/broken.md", "---\ntitle: Broken\nwhen: next tuesday\n---\nBad date.")
	write("content/private/board.md", "---\ntitle: Board meeting\nwhen: 2026-10-12 18:00\n---\nMembers only.")
	write("pages/events/index.html",
		"{% for e in collections.events|upcoming %}[{{ e.title }}|{{ e.when|when }}|/events/{{ e.slug }}]{% endfor %}"+
			"|month:{% for e in collections.events|in_month:\"2026-10\" %}{{ e.title }};{% endfor %}"+
			"|day:{% for e in collections.events|on_day:\"2026-10-13\" %}{{ e.title }};{% endfor %}"+
			"|past:{% for e in collections.events|past %}{{ e.title }};{% endfor %}"+
			"|limit:{% for e in collections.events|upcoming:2 %}{{ e.title }};{% endfor %}")
	write("pages/events/[slug].html", "{{ record.title }}|{{ record.when|when }}|{{ record.when.repeats }}|next:{{ record.when.next|when }}|{{ record.data.mood }}|when-in-data:{{ record.data.when }}|going:{{ record.rsvps.going }}|place:{{ record.location.label }}|g:{{ record|google_calendar_url }}|sub:{{ calendar.google }}")
	write("pages/private/[slug].html", "{{ record.title }}")

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

	// Listing: the weekly series expands (Oct 20 skipped), the past and dateless
	// posts are absent, occurrences are soonest first.
	_, body, _ := get("/events/")
	body = strings.SplitN(body, "\n<script", 2)[0] // drop the dev live-reload tag
	wantList := "[Harvest Fair|Sun Oct 4, 10 am – 4 pm|/events/harvest-fair]" +
		"[Book club|Tue Oct 6, 7 pm – 8:30 pm|/events/book-club]" +
		"[Book club|Tue Oct 13, 7 pm – 8:30 pm|/events/book-club]" +
		"[Book club|Tue Oct 27, 7 pm – 8:30 pm|/events/book-club]"
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
	if !strings.HasPrefix(body, "Book club|Tue Oct 6, 7 pm – 8:30 pm|weekly|next:Tue Oct 6, 7 pm – 8:30 pm||when-in-data:|going:0") {
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
		"|g:https://calendar.google.com/calendar/render?action=TEMPLATE&amp;ctz=America%2FLos_Angeles&amp;dates=20261004T100000%2F20261004T160000&amp;details=Bring+a+dish.&amp;location=Harvest+Fair&amp;text=Harvest+Fair|",
		"|sub:https://calendar.google.com/calendar/r?cid=" + strings.ReplaceAll(strings.ReplaceAll(srv.URL, ":", "%3A"), "/", "%2F") + "%2Fcalendar.ics",
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
		"DTSTART;TZID=America/Los_Angeles:20261006T190000",
		"RRULE:FREQ=WEEKLY",
		"EXDATE;TZID=America/Los_Angeles:20261020T190000",
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
	_, ics, _ = get("/calendar.ics?collection=events&record=" + recordID(t, db, "events", "book-club") + "&occurrence=2026-10-13T19:00:00-07:00")
	if strings.Count(ics, "BEGIN:VEVENT") != 1 || strings.Contains(ics, "RRULE") || !strings.Contains(ics, "20261013T190000") {
		t.Errorf("single instance:\n%s", ics)
	}

	// The JSON feed for the SDK.
	_, js, _ := get("/calendar.json?from=2026-10-01&to=2026-11-01")
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
