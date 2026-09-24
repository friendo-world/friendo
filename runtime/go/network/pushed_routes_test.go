package network

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// TestPushedRouteServesWithoutRestart reproduces what took docs.friendo.world
// down: `friendo deploy` reaches a site's push API — which builds the site's
// handler and its route table — before the templates it's pushing exist. A
// dynamic page arriving in that push (pages/docs/[slug].html) must route on
// the very next request, not after a process restart.
func TestPushedRouteServesWithoutRestart(t *testing.T) {
	n := newTestNetwork(t)
	tok := operatorToken(t, n.accounts, "op@x.com")
	n.api("POST", "/api/sites", tok, `{"subdomain":"docs"}`)

	// The site's handler is built by its first request (as the push itself does).
	if code := n.req("GET", "docs.localhost", "/", "", "").Code; code != http.StatusOK {
		t.Fatalf("first request = %d", code)
	}
	if code := n.req("GET", "docs.localhost", "/docs/auth", "", "").Code; code != http.StatusNotFound {
		t.Fatalf("before the push, /docs/auth = %d, want 404", code)
	}

	// A record for the page to render, and the site session `friendo deploy` pushes with.
	dir, _ := n.reg.Dir("docs")
	db, _ := data.Open(dir)
	db.UpsertRecordBySlug("docs", "auth", "Auth page", "body", "published", "", "{}")
	db.Conn.Close()
	var sso struct {
		Session string `json:"session"`
	}
	json.Unmarshal(n.api("POST", "/api/sso/exchange", tok, `{"subdomain":"docs"}`).Body.Bytes(), &sso)
	if sso.Session == "" {
		t.Fatal("no site session")
	}

	// Push the dynamic page through the real endpoint.
	body := `{"files":[{"path":"pages/docs/[slug].html","content":"<h1>{{ record.title }}</h1>"}]}`
	req := httptest.NewRequest("POST", "http://docs.localhost/_/api/push/templates", strings.NewReader(body))
	req.Host = "docs.localhost"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "friendo_session", Value: sso.Session})
	rec := httptest.NewRecorder()
	n.d.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("push templates = %d\n%s", rec.Code, rec.Body.String())
	}

	// The new route serves immediately.
	rec = n.req("GET", "docs.localhost", "/docs/auth", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Auth page") {
		t.Fatalf("after the push, /docs/auth = %d\n%s", rec.Code, rec.Body.String())
	}
}
