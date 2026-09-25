// CLI sync round-trip: the deploy flow (init → push → pull) is what `friendo push`
// / `pull` run, but it had no automated coverage — only hand-driven checks. This
// drives the real deploy SiteClient (the same type the CLI uses) against an
// in-process Go runtime: bootstrap the admin, push records + file rows, pull them
// back, and assert the data round-trips intact. Complements the REST parity
// scenarios (which hit endpoints individually) by proving push and pull agree.
package deploy

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

func TestCLIPushPullRoundTrip(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(siteDir, "pages"), 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatalf("building site handler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewSiteClient(srv.URL, "")

	// A fresh site needs setup; bootstrapping creates the first admin (as the CLI does).
	needs, err := client.NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("fresh site should need setup")
	}
	if err := client.Setup("admin@test.com", "Admin", "password12345"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Push a record and a gallery file row (the media plumbing from Tier 1).
	records := []map[string]any{{
		"id": "p1", "collection": "blog", "slug": "hello", "title": "Hello",
		"body": "world", "status": "published", "created": "2020-01-01T00:00:00Z",
	}}
	events := []map[string]any{{
		"id": "e1", "target_type": "post", "target_id": "p1",
		"starts": "2026-10-04T10:00:00-07:00", "ends": "2026-10-04T16:00:00-07:00",
		"all_day": false, "timezone": "America/Los_Angeles", "rrule": "FREQ=WEEKLY",
		"exdates": []string{"2026-10-11"}, "created": "2020-01-01T00:00:00Z",
	}}
	if err := client.PushData(records, events); err != nil {
		t.Fatalf("PushData: %v", err)
	}
	files := []map[string]any{{
		"id": "f1", "record_type": "post", "record_id": "p1", "field": "gallery",
		"r2_key": "assets/galleries/blog/hello/a.png", "mime": "image/png",
		"size": 10, "created": "2020-01-01T00:00:00Z",
	}}
	if err := client.PushFiles(files); err != nil {
		t.Fatalf("PushFiles: %v", err)
	}

	// Pull it back and assert the round-trip.
	gotRecords, gotEvents, err := client.PullDataAndEvents()
	if err != nil {
		t.Fatalf("PullData: %v", err)
	}
	ev := findByID(gotEvents, "e1")
	if ev == nil || ev["target_id"] != "p1" || ev["rrule"] != "FREQ=WEEKLY" || ev["starts"] != "2026-10-04T10:00:00-07:00" {
		t.Fatalf("event round-trip mismatch: %#v", gotEvents)
	}
	rec := findByID(gotRecords, "p1")
	if rec == nil {
		t.Fatalf("pulled records missing p1: %#v", gotRecords)
	}
	if rec["title"] != "Hello" || rec["slug"] != "hello" || rec["status"] != "published" {
		t.Fatalf("record round-trip mismatch: %#v", rec)
	}

	// Users round-trip with their password hash intact — the thing `push --users`
	// promises ("the same password works everywhere").
	gotUsers, _, err := client.PullUsers()
	if err != nil {
		t.Fatalf("PullUsers: %v", err)
	}
	var adminRow map[string]any
	for _, u := range gotUsers {
		if u["email"] == "admin@test.com" {
			adminRow = u
		}
	}
	if adminRow == nil {
		t.Fatalf("pulled users missing admin: %#v", gotUsers)
	}
	if h, _ := adminRow["password_hash"].(string); !strings.HasPrefix(h, "$2") {
		t.Fatalf("pulled admin has no bcrypt hash: %#v", adminRow)
	}
	if err := client.PushUsers(gotUsers, nil); err != nil {
		t.Fatalf("PushUsers: %v", err)
	}
	if _, err := db.AuthenticateUser("admin@test.com", "password12345"); err != nil {
		t.Fatalf("password should survive a push of pulled users: %v", err)
	}

	gotFiles, err := client.PullFiles()
	if err != nil {
		t.Fatalf("PullFiles: %v", err)
	}
	f := findByID(gotFiles, "f1")
	if f == nil {
		t.Fatalf("pulled files missing f1: %#v", gotFiles)
	}
	if f["field"] != "gallery" || f["record_id"] != "p1" || f["r2_key"] != "assets/galleries/blog/hello/a.png" {
		t.Fatalf("file round-trip mismatch: %#v", f)
	}
}

// findByID returns the row whose "id" equals id, or nil.
func findByID(rows []map[string]any, id string) map[string]any {
	for _, r := range rows {
		if r["id"] == id {
			return r
		}
	}
	return nil
}
