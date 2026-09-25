package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// A static export leaves members-only pages out — tagged pages, tagged dynamic
// pages, and paths named in friendo.toml's [access] — and uses the site's name.
func TestStaticExportSkipsMembersOnlyPages(t *testing.T) {
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"Exported\"\n\n[access]\nmembers_only = [\"/private/*\"]\n")
	write("pages/index.html", "home of {{ site.name }}")
	write("pages/members.html", "{% members only %}secret")
	write("pages/private/index.html", "also secret")
	write("pages/blog/[slug].html", "{% members only %}{{ record.title }}")
	write("pages/notes/[slug].html", "{{ record.title }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"blog", "notes"} {
		if _, err := db.CreateRecord(c, "hello", "Hello", "body", "published", ""); err != nil {
			t.Fatalf("create %s record: %v", c, err)
		}
	}
	db.Close()

	t.Chdir(siteDir)
	if err := Run("static"); err != nil {
		t.Fatalf("export: %v", err)
	}
	dist := filepath.Join(siteDir, "dist")
	home, err := os.ReadFile(filepath.Join(dist, "index.html"))
	if err != nil || string(home) != "home of Exported" {
		t.Fatalf("index.html = %q, %v", home, err)
	}
	if _, err := os.Stat(filepath.Join(dist, "notes", "hello", "index.html")); err != nil {
		t.Fatalf("public dynamic page missing: %v", err)
	}
	for _, gated := range []string{"members/index.html", "private/index.html", "blog/hello/index.html"} {
		if _, err := os.Stat(filepath.Join(dist, gated)); err == nil {
			t.Errorf("%s was exported; members-only pages must be skipped", gated)
		}
	}
}

// A static export writes the calendar feeds — published events only, minus any
// members-only collection — so a static site is subscribable.
func TestStaticExportWritesCalendarFeeds(t *testing.T) {
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"Exported\"\ntimezone = \"Europe/Paris\"\n\n[deploy]\ndomain = \"village.example\"\n")
	write("pages/index.html", "{% for e in collections.events|upcoming %}{{ e.title }}{% endfor %}")
	write("pages/events/[slug].html", "{{ record.title }} {{ record.when|when }}")
	write("pages/private/[slug].html", "{% members only %}{{ record.title }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	add := func(collection, slug, status, when string) {
		id, err := db.CreateRecord(collection, slug, slug, "", status, "")
		if err != nil {
			t.Fatal(err)
		}
		ev, _, _ := data.ParseWhen(map[string]any{"when": when}, db.Location)
		db.ReconcileWhen(id, ev)
	}
	// Two months out, so the feeds' default window includes it.
	day := time.Now().In(db.Location).AddDate(0, 2, 0)
	fairDay := day.Format("2006-01-02")
	add("events", "fair", "published", fairDay+" 10:00 to 16:00")
	add("events", "draft", "draft", fairDay)
	add("private", "board", "published", fairDay+" 18:00")
	wantWhen := data.FormatWhen(
		time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, db.Location),
		time.Date(day.Year(), day.Month(), day.Day(), 16, 0, 0, 0, db.Location), false, time.Now().In(db.Location), "")
	db.Close()

	t.Chdir(siteDir)
	if err := Run("static"); err != nil {
		t.Fatalf("export: %v", err)
	}
	dist := filepath.Join(siteDir, "dist")
	ics, err := os.ReadFile(filepath.Join(dist, "calendar.ics"))
	if err != nil {
		t.Fatalf("calendar.ics missing: %v", err)
	}
	s := strings.ReplaceAll(string(ics), "\r\n ", "")
	if strings.Count(s, "BEGIN:VEVENT") != 1 || !strings.Contains(s, "SUMMARY:fair") {
		t.Errorf("ics should carry the one published, public event:\n%s", s)
	}
	if !strings.Contains(s, "DTSTART;TZID=Europe/Paris:"+day.Format("20060102")+"T100000") || !strings.Contains(s, "URL:https://village.example/events/fair") {
		t.Errorf("ics zone/url:\n%s", s)
	}
	js, err := os.ReadFile(filepath.Join(dist, "calendar.json"))
	if err != nil || !strings.Contains(string(js), `"slug": "fair"`) || strings.Contains(string(js), "board") {
		t.Errorf("calendar.json: %v\n%s", err, js)
	}
	page, _ := os.ReadFile(filepath.Join(dist, "events", "fair", "index.html"))
	if !strings.Contains(string(page), "fair "+wantWhen) {
		t.Errorf("event page: %s", page)
	}
}
