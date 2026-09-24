// Covers friendo.toml's [settings] block as the source of truth: keys declared there
// are applied to the DB on mount and reported read-only, and the settings API refuses
// to change them while still allowing the keys the file leaves out.
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

func TestTomlManagedSettings(t *testing.T) {
	siteDir := t.TempDir()
	toml := `[site]
name = "testsite"

[settings]
accept_submissions = true
require_approval = true
default_role = "contributor"
`
	if err := os.WriteFile(filepath.Join(siteDir, "friendo.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("writing friendo.toml: %v", err)
	}

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	r := chi.NewRouter()
	// Mount reads friendo.toml from siteDir and applies [settings] to the DB.
	admin.Mount(r, db, false, "testsite", siteDir, nil, nil, nil)

	// The declared keys are now the DB's values, regardless of prior state.
	if !db.GetBoolSetting("content.accept_submissions", false) {
		t.Fatal("accept_submissions from [settings] was not applied to the DB")
	}
	if !db.GetBoolSetting("content.require_approval", false) {
		t.Fatal("require_approval from [settings] was not applied to the DB")
	}
	if got := db.GetSetting("access.default_role", "member"); got != "contributor" {
		t.Fatalf("default_role = %q, want contributor", got)
	}

	srv := httptest.NewServer(r)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base := srv.URL + "/_/api"

	do := func(method, path string, body any) map[string]any {
		var rdr io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, rdr)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})

	// GET /settings reports the managed keys and the file's values.
	s := do("GET", "/settings", nil)
	managed := map[string]bool{}
	for _, k := range s["managed"].([]any) {
		managed[k.(string)] = true
	}
	for _, want := range []string{"content.accept_submissions", "content.require_approval", "access.default_role"} {
		if !managed[want] {
			t.Fatalf("managed list %v is missing %q", s["managed"], want)
		}
	}
	if managed["moderation.auto_approve"] {
		t.Fatal("auto_approve should not be managed (it wasn't declared in [settings])")
	}

	// A PUT that tries to flip a managed key is ignored, but an unmanaged key changes.
	do("PUT", "/settings", map[string]any{
		"content":    map[string]any{"accept_submissions": false},
		"moderation": map[string]any{"auto_approve": true},
	})
	after := do("GET", "/settings", nil)
	if after["content"].(map[string]any)["accept_submissions"] != true {
		t.Fatal("managed accept_submissions was changed via the API — it should be frozen")
	}
	if after["moderation"].(map[string]any)["auto_approve"] != true {
		t.Fatal("unmanaged auto_approve should have changed via the API")
	}
}
