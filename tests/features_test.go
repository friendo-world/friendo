// Covers the feature switches (Settings → Features): turning one off makes its
// API answer 403 with `off: true` for reads and writes alike while the others
// keep working, the public /features endpoint reports the state, friendo.toml's
// [settings] can freeze a switch, and the built-in collections a site shows can
// be narrowed.
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

func featuresServer(t *testing.T, toml string) (*httptest.Server, func(method, path, body string) (int, map[string]any)) {
	t.Helper()
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "friendo.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("writing friendo.toml: %v", err)
	}
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	r := chi.NewRouter()
	admin.Mount(r, db, true, "testsite", siteDir, nil, nil, nil)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader([]byte(body)))
		req.Header.Set("X-Friendo-Admin", "1")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		out := map[string]any{}
		json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	return srv, call
}

func TestFeatureSwitches(t *testing.T) {
	_, call := featuresServer(t, "[site]\nname = \"f\"\n")

	// Everything starts on.
	code, out := call("GET", "/_/api/features", "")
	if code != 200 || out["features"].(map[string]any)["comments"] != true {
		t.Fatalf("GET /features = %d %v", code, out)
	}
	if code, _ := call("GET", "/_/api/posts/nope/comments", ""); code != 200 {
		t.Fatalf("comments should be reachable while on, got %d", code)
	}

	// Turn comments off.
	code, out = call("PUT", "/_/api/settings", `{"features":{"comments":false}}`)
	if code != 200 || out["features"].(map[string]any)["comments"] != false {
		t.Fatalf("PUT /settings = %d %v", code, out)
	}
	code, out = call("GET", "/_/api/features", "")
	if out["features"].(map[string]any)["comments"] != false || out["features"].(map[string]any)["reactions"] != true {
		t.Fatalf("features after switch = %v", out)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/_/api/posts/nope/comments"},
		{"POST", "/_/api/posts/nope/comments"},
		{"GET", "/_/api/comments?status=pending"},
	} {
		code, out := call(c.method, c.path, `{"body":"hi"}`)
		if code != 403 || out["off"] != true {
			t.Fatalf("%s %s while off = %d %v, want 403 with off:true", c.method, c.path, code, out)
		}
	}
	if code, _ := call("GET", "/_/api/reactions?target_type=post&target_id=nope", ""); code != 200 {
		t.Fatalf("reactions should stay on, got %d", code)
	}

	// Unknown feature names are ignored, not stored.
	code, _ = call("PUT", "/_/api/settings", `{"features":{"teleport":false}}`)
	if code != 200 {
		t.Fatalf("PUT with an unknown feature = %d", code)
	}

	// Narrow the built-in collections.
	code, out = call("PUT", "/_/api/settings", `{"content":{"default_collections":["blog","nonsense"]}}`)
	if code != 200 {
		t.Fatalf("PUT default_collections = %d", code)
	}
	if got := out["content"].(map[string]any)["default_collections"]; len(got.([]any)) != 1 || got.([]any)[0] != "blog" {
		t.Fatalf("default_collections = %v, want [blog]", got)
	}
	_, out = call("GET", "/_/api/collections", "")
	names := []string{}
	for _, c := range out["collections"].([]any) {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	if strings.Join(names, " ") != "blog" {
		t.Fatalf("collections = %q, want just blog", names)
	}
}

func TestFeatureSwitchFrozenByToml(t *testing.T) {
	_, call := featuresServer(t, "[site]\nname = \"f\"\n\n[settings]\ncomments = false\ndefault_collections = [\"pages\"]\n")
	_, out := call("GET", "/_/api/settings", "")
	managed := out["managed"].([]any)
	has := func(k string) bool {
		for _, m := range managed {
			if m == k {
				return true
			}
		}
		return false
	}
	if !has("features.comments") || !has("content.default_collections") {
		t.Fatalf("managed = %v, want features.comments and content.default_collections", managed)
	}
	code, out := call("PUT", "/_/api/settings", `{"features":{"comments":true},"content":{"default_collections":["blog"]}}`)
	if code != 200 || out["features"].(map[string]any)["comments"] != false {
		t.Fatalf("a frozen switch should not flip: %d %v", code, out)
	}
	if got := out["content"].(map[string]any)["default_collections"]; got.([]any)[0] != "pages" {
		t.Fatalf("frozen default_collections = %v", got)
	}
	if code, _ := call("GET", "/_/api/posts/nope/comments", ""); code != 403 {
		t.Fatalf("comments should be off from the toml, got %d", code)
	}
}
