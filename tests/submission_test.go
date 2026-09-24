// Covers the <friendo-form> community-submission surface's rate limit, which the
// declarative scenarios.json can't reach (it needs ~20 identical requests to trip
// the durable limiter). A member submitting posts is capped at the same 20-per-5min
// bucket as comments/messages; the 21st submission returns 429.
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

func TestMemberSubmissionRateLimit(t *testing.T) {
	db, err := data.Open(t.TempDir())
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	r := chi.NewRouter()
	admin.Mount(r, db, false, "testsite", t.TempDir(), nil, nil, nil)
	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base := srv.URL + "/_/api"

	do := func(method, path string, body any) *http.Response {
		var rdr io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, base+path, rdr)
		if err != nil {
			t.Fatalf("%s %s: building request: %v", method, path, err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	want := func(resp *http.Response, status int, ctx string) {
		if resp.StatusCode != status {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("%s: status = %d, want %d\nbody: %s", ctx, resp.StatusCode, status, b)
		}
		resp.Body.Close()
	}

	// First-run owner (the jar keeps the session cookie), open submissions, and a
	// plain member to submit as.
	want(do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"}), 201, "setup")
	want(do("PUT", "/settings", map[string]any{"content": map[string]any{"accept_submissions": true}}), 200, "enable submissions")
	want(do("POST", "/users", map[string]any{"email": "m@test.com", "name": "M", "password": "password12345", "role": "member"}), 201, "create member")
	want(do("POST", "/auth/login", map[string]any{"email": "m@test.com", "password": "password12345"}), 200, "member login")

	// The bucket allows 20 submissions per window; the 21st is rejected.
	for i := 0; i < 20; i++ {
		resp := do("POST", "/collections/blog/records", map[string]any{"title": "Post", "body": "hi"})
		want(resp, 201, "submission within limit")
	}
	resp := do("POST", "/collections/blog/records", map[string]any{"title": "One too many", "body": "hi"})
	want(resp, http.StatusTooManyRequests, "submission over limit")
}
