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
members_can_post = true
posts_need_review = true
signups_are_contributors = true
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
	if !db.GetBoolSetting("members_can_post", false) {
		t.Fatal("members_can_post from [settings] was not applied to the DB")
	}
	if !db.GetBoolSetting("posts_need_review", false) {
		t.Fatal("posts_need_review from [settings] was not applied to the DB")
	}
	if !db.GetBoolSetting("signups_are_contributors", false) {
		t.Fatal("signups_are_contributors from [settings] was not applied to the DB")
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
	for _, want := range []string{"members_can_post", "posts_need_review", "signups_are_contributors"} {
		if !managed[want] {
			t.Fatalf("managed list %v is missing %q", s["managed"], want)
		}
	}
	if managed["comments_need_review"] {
		t.Fatal("comments_need_review should not be managed (it wasn't declared in [settings])")
	}

	// A PUT that tries to flip a managed key is ignored, but an unmanaged key changes.
	do("PUT", "/settings", map[string]any{"members_can_post": false, "comments_need_review": false})
	after := do("GET", "/settings", nil)
	if after["members_can_post"] != true {
		t.Fatal("managed members_can_post was changed via the API — it should be frozen")
	}
	if after["comments_need_review"] != false {
		t.Fatal("unmanaged comments_need_review should have changed via the API")
	}
}
