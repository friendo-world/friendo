package calendar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// A site in Los Angeles with four kinds of event: a one-off with a pin, an
// all-day multi-day, a weekly series with a skipped date, and one in a
// members-only collection that must not appear.
func fixtureSite(t *testing.T) *data.DB {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "friendo.toml"), []byte("[site]\nname = \"Neighbourhood; Events, Inc.\"\ntimezone = \"America/Los_Angeles\"\n"), 0o644)
	db, err := data.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	add := func(collection, slug, title, body, status string, meta map[string]any) string {
		id, err := db.UpsertRecordBySlug(collection, slug, title, body, status, "", "{}")
		if err != nil {
			t.Fatal(err)
		}
		db.Conn.Exec(`UPDATE posts SET updated = '2026-09-01T12:00:00Z' WHERE id = ?`, id)
		ev, warnings, _ := data.ParseWhen(meta, db.Location)
		if len(warnings) > 0 {
			t.Fatalf("%s: %v", slug, warnings)
		}
		if err := db.ReconcileWhen(id, ev); err != nil {
			t.Fatal(err)
		}
		db.Conn.Exec(`UPDATE events SET updated = '2026-09-01T12:00:00Z' WHERE target_id = ?`, id)
		return id
	}
	fair := add("events", "harvest-fair", "Harvest Fair", "Bring a dish, **music** from noon.\n\nAll welcome; kids too.", "published",
		map[string]any{"when": "2026-10-04 10:00 to 16:00"})
	db.UpsertLocation(data.DeterministicLocationID(fair), "post", fair, 47.6062, -122.3321, "Pike Place Market")
	add("events", "retreat", "Retreat", "", "published", map[string]any{"when": "2026-11-06 to 2026-11-08"})
	add("events", "book-club", "Book club", "Every Tuesday.", "published",
		map[string]any{"when": "2026-10-06 19:00 to 20:30", "repeats": "weekly", "except": []any{"2026-11-24"}})
	add("events", "draft-thing", "Draft", "", "draft", map[string]any{"when": "2026-10-10"})
	add("private", "board-meeting", "Board meeting", "", "published", map[string]any{"when": "2026-10-12 18:00"})
	add("blog", "no-time", "Just a post", "", "published", map[string]any{"title": "x"})
	return db
}

func visibleExceptPrivate(c string) bool { return c != "private" }

func permalink(collection string, f map[string]string) string {
	if collection == "blog" {
		return ""
	}
	return "/" + collection + "/" + f["slug"]
}

func TestCollect(t *testing.T) {
	db := fixtureSite(t)
	entries, err := Collect(db, visibleExceptPrivate, permalink, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, e := range entries {
		slugs = append(slugs, e.Slug)
	}
	if strings.Join(slugs, ",") != "harvest-fair,book-club,retreat" {
		t.Errorf("entries = %v (want published, visible, soonest first)", slugs)
	}
	if entries[0].Path != "/events/harvest-fair" || !entries[0].HasPlace || entries[0].Place != "Pike Place Market" {
		t.Errorf("first entry: %+v", entries[0])
	}
	only, _ := Collect(db, nil, permalink, Filter{Collection: "private"})
	if len(only) != 1 || only[0].Slug != "board-meeting" {
		t.Errorf("collection filter: %+v", only)
	}
}

func TestICSGolden(t *testing.T) {
	db := fixtureSite(t)
	entries, _ := Collect(db, visibleExceptPrivate, permalink, Filter{Collection: "events"})
	var b strings.Builder
	if err := WriteICS(&b, entries, ICSOptions{SiteName: "Neighbourhood; Events, Inc.", SiteZone: db.Location, BaseURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.HasSuffix(got, "END:VCALENDAR\r\n") || !strings.HasPrefix(got, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n") {
		t.Errorf("envelope:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line longer than 75 octets: %q", line)
		}
	}
	// Compare against the unfolded text (a long property spans several lines).
	unfolded := strings.ReplaceAll(got, "\r\n ", "")
	// The VTIMEZONE names the zone and carries both offsets.
	for _, want := range []string{
		"X-WR-CALNAME:Neighbourhood\\; Events\\, Inc.",
		"X-WR-TIMEZONE:America/Los_Angeles",
		"BEGIN:VTIMEZONE\r\nTZID:America/Los_Angeles",
		"TZOFFSETFROM:-0700\r\nTZOFFSETTO:-0800\r\nTZNAME:PST",
		"TZOFFSETFROM:-0800\r\nTZOFFSETTO:-0700\r\nTZNAME:PDT",
		// One-off with a pin.
		"DTSTART;TZID=America/Los_Angeles:20261004T100000\r\nDTEND;TZID=America/Los_Angeles:20261004T160000",
		"SUMMARY:Harvest Fair",
		"URL:https://example.test/events/harvest-fair",
		"LOCATION:Pike Place Market",
		"GEO:47.606200;-122.332100",
		"CATEGORIES:events",
		// Markdown became text; the URL is appended; escaping applied.
		"DESCRIPTION:Bring a dish\\, music from noon.\\n\\nAll welcome\\; kids too.\\n\\nhttps://ex",
		// All-day multi-day: DATE values, exclusive end.
		"DTSTART;VALUE=DATE:20261106\r\nDTEND;VALUE=DATE:20261109",
		// Weekly with a skipped date at the series' time of day.
		"RRULE:FREQ=WEEKLY",
		"EXDATE;TZID=America/Los_Angeles:20261124T190000",
		"LAST-MODIFIED:20260901T120000Z",
	} {
		if !strings.Contains(unfolded, want) {
			t.Errorf("missing %q in:\n%s", want, unfolded)
		}
	}
	if strings.Count(got, "BEGIN:VEVENT") != 3 {
		t.Errorf("want 3 VEVENTs:\n%s", got)
	}
	if strings.Contains(got, "Board meeting") || strings.Contains(got, "Draft") {
		t.Errorf("gated or unpublished post leaked:\n%s", got)
	}
}

func TestICSSingleOccurrence(t *testing.T) {
	db := fixtureSite(t)
	entries, _ := Collect(db, nil, permalink, Filter{Collection: "events"})
	var series *Entry
	for i := range entries {
		if entries[i].Slug == "book-club" {
			series = &entries[i]
		}
	}
	var b strings.Builder
	WriteICS(&b, []Entry{*series}, ICSOptions{SiteZone: db.Location, Occurrence: "2026-10-13T19:00:00-07:00"})
	got := b.String()
	if strings.Contains(got, "RRULE") || !strings.Contains(got, "DTSTART;TZID=America/Los_Angeles:20261013T190000") {
		t.Errorf("single occurrence:\n%s", got)
	}
	if !strings.Contains(got, "UID:"+series.Event.ID+"-20261013T190000@friendo") {
		t.Errorf("instance uid:\n%s", got)
	}
	// A time that isn't an occurrence (the skipped date) yields nothing.
	b.Reset()
	WriteICS(&b, []Entry{*series}, ICSOptions{SiteZone: db.Location, Occurrence: "2026-11-24T19:00:00-08:00"})
	if strings.Contains(b.String(), "BEGIN:VEVENT") {
		t.Error("skipped date must not produce an instance")
	}
}

func TestOccurrencesJSON(t *testing.T) {
	db := fixtureSite(t)
	entries, _ := Collect(db, visibleExceptPrivate, permalink, Filter{})
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, db.Location)
	occs := Occurrences(entries, from, from.AddDate(0, 1, 0), "https://example.test")
	var titles []string
	for _, o := range occs {
		titles = append(titles, o["title"].(string)+"@"+o["starts"].(string)[:10])
	}
	want := "Harvest Fair@2026-10-04,Book club@2026-10-06,Book club@2026-10-13,Book club@2026-10-20,Book club@2026-10-27"
	if strings.Join(titles, ",") != want {
		t.Errorf("october:\n got %v\nwant %v", titles, want)
	}
	if occs[0]["url"] != "https://example.test/events/harvest-fair" || occs[0]["place"] != "Pike Place Market" || occs[1]["repeats"] != "weekly" {
		t.Errorf("fields: %+v", occs[:2])
	}
}

func TestFeedHTTP(t *testing.T) {
	db := fixtureSite(t)
	feed := Feed{DB: db, SiteName: "Test", Visible: visibleExceptPrivate, Permalink: permalink}
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar.ics", feed.ServeICS)
	mux.HandleFunc("/calendar.json", feed.ServeJSON)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/calendar.ics")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/calendar") {
		t.Fatalf("ics: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	tag := res.Header.Get("ETag")
	if tag == "" || res.Header.Get("Cache-Control") == "" {
		t.Error("ics needs ETag + Cache-Control")
	}
	res.Body.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/calendar.ics", nil)
	req.Header.Set("If-None-Match", tag)
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match: %d, want 304", res.StatusCode)
	}
	res.Body.Close()

	// The URL is absolute, from the request's host.
	res, _ = http.Get(srv.URL + "/calendar.ics?collection=events")
	body := make([]byte, 1<<16)
	n, _ := res.Body.Read(body)
	res.Body.Close()
	if !strings.Contains(strings.ReplaceAll(string(body[:n]), "\r\n ", ""), "URL:"+srv.URL+"/events/harvest-fair") {
		t.Errorf("absolute URL missing:\n%s", body[:n])
	}

	res, _ = http.Get(srv.URL + "/calendar.json?from=2026-10-01&to=2026-10-31")
	var payload struct {
		Timezone string           `json:"timezone"`
		Events   []map[string]any `json:"events"`
	}
	json.NewDecoder(res.Body).Decode(&payload)
	res.Body.Close()
	if payload.Timezone != "America/Los_Angeles" || len(payload.Events) != 5 {
		t.Errorf("json: %+v", payload)
	}
}

func TestPlainTextAndFold(t *testing.T) {
	if got := plainText("# Title\n\nSome *emphasis* and a [link](http://x). \n\n- one\n- two"); got != "Title\n\nSome emphasis and a link.\n\none\ntwo" {
		t.Errorf("plainText = %q", got)
	}
	long := "SUMMARY:" + strings.Repeat("é", 60)
	folded := fold(long)
	for _, l := range strings.Split(folded, "\r\n") {
		if len(l) > 75 {
			t.Errorf("fold left a %d-octet line", len(l))
		}
	}
	if strings.ReplaceAll(folded, "\r\n ", "") != long {
		t.Error("fold must be reversible")
	}
}

func TestWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Paris")
	from, to := Window("2026-10", "", loc)
	if from.Format("2006-01-02") != "2026-10-01" || to.Sub(from) < 360*24*time.Hour {
		t.Errorf("month default: %v → %v", from, to)
	}
	from, to = Window("2026-01-01", "2030-01-01", loc)
	if to.Year() != 2028 {
		t.Errorf("cap at two years: %v", to)
	}
}

func TestLinksAndGoogleURL(t *testing.T) {
	l := Links("https://village.example")
	if l["ics"] != "https://village.example/calendar.ics" || l["webcal"] != "webcal://village.example/calendar.ics" {
		t.Errorf("links: %v", l)
	}
	if l["google"] != "https://calendar.google.com/calendar/r?cid=https%3A%2F%2Fvillage.example%2Fcalendar.ics" {
		t.Errorf("google subscribe: %s", l["google"])
	}
	if l := Links(""); l["google"] != "" || l["ics"] != "/calendar.ics" {
		t.Errorf("no origin: %v", l)
	}
	loc, _ := time.LoadLocation("America/Los_Angeles")
	u := GoogleEventURL("Harvest Fair", time.Date(2026, 10, 4, 10, 0, 0, 0, loc), time.Date(2026, 10, 4, 16, 0, 0, 0, loc), false, "FREQ=WEEKLY", "Bring a dish", "Pike Place")
	for _, want := range []string{"action=TEMPLATE", "text=Harvest+Fair", "dates=20261004T100000%2F20261004T160000", "ctz=America%2FLos_Angeles", "recur=RRULE%3AFREQ%3DWEEKLY", "location=Pike+Place", "details=Bring+a+dish"} {
		if !strings.Contains(u, want) {
			t.Errorf("google event url missing %s: %s", want, u)
		}
	}
	u = GoogleEventURL("Retreat", time.Date(2026, 11, 6, 0, 0, 0, 0, loc), time.Date(2026, 11, 8, 0, 0, 0, 0, loc), true, "", "", "")
	if !strings.Contains(u, "dates=20261106%2F20261109") || strings.Contains(u, "ctz=") {
		t.Errorf("all-day: %s", u)
	}
}
