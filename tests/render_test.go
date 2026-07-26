// Render-layer regression: parity_test.go proves the REST API behaves correctly;
// this proves the *template engine* does. Each render-scenarios.json fixture's
// rendered output must equal its golden `expect`. Fixtures are fragments (no
// </body>) so the serve path's dev-only live-reload injection stays a no-op.
package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

type renderFixture struct {
	Name     string           `json:"name"`
	Template string           `json:"template"`
	Records  []map[string]any `json:"records"`
	Expect   string           `json:"expect"`
}

func TestRenderParity(t *testing.T) {
	raw, err := os.ReadFile("render-scenarios.json")
	if err != nil {
		t.Fatalf("reading render-scenarios.json: %v", err)
	}
	var fixtures []renderFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("parsing render-scenarios.json: %v", err)
	}

	// One temp site + DB holds every fixture's template and records; fixtures use
	// distinct collections so they don't interfere.
	siteDir := t.TempDir()
	pagesDir := filepath.Join(siteDir, "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	for _, f := range fixtures {
		page := filepath.Join(pagesDir, f.Name+".html")
		if err := os.WriteFile(page, []byte(f.Template), 0o644); err != nil {
			t.Fatalf("%s: writing template: %v", f.Name, err)
		}
		for _, rec := range f.Records {
			insertFixtureRecord(t, db, f.Name, rec)
		}
	}

	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatalf("building site handler: %v", err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	for _, f := range fixtures {
		resp, err := http.Get(srv.URL + "/" + f.Name)
		if err != nil {
			t.Fatalf("%s: GET failed: %v", f.Name, err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200\nbody: %s", f.Name, resp.StatusCode, bodyBytes)
		}
		if got := string(bodyBytes); got != f.Expect {
			t.Fatalf("%s: render mismatch\n got: %q\nwant: %q", f.Name, got, f.Expect)
		}
	}
}

// insertFixtureRecord writes a fixture record straight into posts with an explicit
// `created` (so `created DESC` ordering is deterministic and matches the edge push).
// id is derived from the collection+slug so it's stable and collision-free.
func insertFixtureRecord(t *testing.T, db *data.DB, fixture string, rec map[string]any) {
	t.Helper()
	str := func(k string) string { v, _ := rec[k].(string); return v }

	dataJSON := "{}"
	if d, ok := rec["data"]; ok && d != nil {
		if b, err := json.Marshal(d); err == nil {
			dataJSON = string(b)
		}
	}

	id := str("collection") + "-" + str("slug")
	created := str("created")
	_, err := db.Conn.Exec(
		`INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, data, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)`,
		id, db.SiteID, str("collection"), str("slug"), str("title"), str("body"),
		str("status"), str("published_at"), dataJSON, created, created,
	)
	if err != nil {
		t.Fatalf("%s: inserting record %s: %v", fixture, id, err)
	}
}
