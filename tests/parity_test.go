// Package tests runs scenarios.json against the runtime's REST API and asserts the
// responses — a regression suite over the API's behavior. (These fixtures were once
// a Go/edge parity harness; the edge runtime is gone, so they now pin the one
// runtime.)
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

type step struct {
	Name    string            `json:"name"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    map[string]any    `json:"body"`
	Expect  expectation       `json:"expect"`
	Capture map[string]string `json:"capture"`
}

type expectation struct {
	Status int                `json:"status"`
	JSON   map[string]any     `json:"json"`
	Length map[string]float64 `json:"length"`
}

func TestParity(t *testing.T) {
	// The parity scenarios read OTP codes from request-code responses, so enable
	// the dev-only echo (off by default in production).
	t.Setenv("FRIENDO_OTP_ECHO", "1")

	raw, err := os.ReadFile("scenarios.json")
	if err != nil {
		t.Fatalf("reading scenarios.json: %v", err)
	}
	var steps []step
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatalf("parsing scenarios.json: %v", err)
	}

	// Mount the real router exactly as the server does, over a fresh temp DB.
	db, err := data.Open(t.TempDir())
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	r := chi.NewRouter()
	// nil permalink resolver: this harness mounts no page routes, so there are no
	// public URLs to resolve (the aggregate locations endpoint simply omits them).
	admin.Mount(r, db, false, "testsite", t.TempDir(), nil, nil, nil)
	srv := httptest.NewServer(r)
	defer srv.Close()

	base := srv.URL + "/_/api"
	vars := map[string]string{}
	var cookie *string

	for i, s := range steps {
		label := fmt.Sprintf("step %d (%s)", i+1, s.Name)

		var bodyReader io.Reader
		if s.Body != nil {
			b, _ := json.Marshal(s.Body)
			bodyReader = bytes.NewReader([]byte(substitute(string(b), vars)))
		}
		req, err := http.NewRequest(s.Method, base+substitute(s.Path, vars), bodyReader)
		if err != nil {
			t.Fatalf("%s: building request: %v", label, err)
		}
		if s.Body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.Header.Set("Cookie", "friendo_session="+*cookie)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: request failed: %v", label, err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Capture any session cookie the server set (including logout's clear).
		for _, ck := range resp.Cookies() {
			if ck.Name == "friendo_session" {
				v := ck.Value
				cookie = &v
			}
		}

		if resp.StatusCode != s.Expect.Status {
			t.Fatalf("%s: status = %d, want %d\nbody: %s", label, resp.StatusCode, s.Expect.Status, bodyBytes)
		}

		var parsed any
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &parsed)
		}

		for path, want := range s.Expect.JSON {
			got, ok := resolvePath(parsed, path)
			if !ok {
				t.Fatalf("%s: json path %q not found\nbody: %s", label, path, bodyBytes)
			}
			wantNorm := want
			if ws, isStr := want.(string); isStr {
				wantNorm = substitute(ws, vars)
			}
			if !jsonEqual(wantNorm, got) {
				t.Fatalf("%s: json[%q] = %v, want %v", label, path, got, wantNorm)
			}
		}

		for path, wantLen := range s.Expect.Length {
			got, ok := resolvePath(parsed, path)
			arr, isArr := got.([]any)
			if !ok || !isArr {
				t.Fatalf("%s: length path %q is not an array\nbody: %s", label, path, bodyBytes)
			}
			if float64(len(arr)) != wantLen {
				t.Fatalf("%s: len(%q) = %d, want %v", label, path, len(arr), wantLen)
			}
		}

		for name, path := range s.Capture {
			got, ok := resolvePath(parsed, path)
			if !ok {
				t.Fatalf("%s: capture path %q not found", label, path)
			}
			vars[name] = fmt.Sprintf("%v", got)
		}
	}
}

// substitute replaces ${name} with the captured variable value.
func substitute(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// resolvePath walks a dotted path (with numeric array indices) through decoded JSON.
func resolvePath(data any, path string) (any, bool) {
	cur := data
	for _, seg := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(c) {
				return nil, false
			}
			cur = c[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// jsonEqual compares two decoded JSON values by their canonical encoding.
func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
