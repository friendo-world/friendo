package network

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// get drives one request through the dispatcher with the given Host header.
func get(t *testing.T, d *Dispatcher, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	return rec
}

// TestDispatcherRoutingAndIsolation provisions two sites and asserts that each
// subdomain serves its own content and cannot see its neighbour's — the core
// multi-tenant guarantee of network mode.
func TestDispatcherRoutingAndIsolation(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Provision("alice", "Alice's Blog"); err != nil {
		t.Fatalf("provision alice: %v", err)
	}
	if _, err := reg.Provision("bob", "Bob's Corner"); err != nil {
		t.Fatalf("provision bob: %v", err)
	}
	// A post that exists only on Alice's site.
	aliceOnly := filepath.Join(root, "alice", "content", "blog", "alice-only.md")
	if err := os.WriteFile(aliceOnly, []byte("---\ntitle: Alice Only Post\nslug: alice-only\n---\nsecret\n"), 0o644); err != nil {
		t.Fatalf("write alice post: %v", err)
	}

	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()

	// Apex with no home site: the landing page, which names no tenant.
	apex := get(t, d, "localhost", "/")
	if apex.Code != http.StatusOK || !strings.Contains(apex.Body.String(), "friendo network") {
		t.Fatalf("apex status = %d, want the landing page", apex.Code)
	}
	for _, leak := range []string{"alice.localhost", "bob.localhost"} {
		if strings.Contains(apex.Body.String(), leak) {
			t.Errorf("landing page must not list %q", leak)
		}
	}

	// Alice serves her own name + her exclusive post.
	aliceHome := get(t, d, "alice.localhost", "/")
	if !strings.Contains(aliceHome.Body.String(), "Alice&#39;s Blog") && !strings.Contains(aliceHome.Body.String(), "Alice's Blog") {
		t.Errorf("alice homepage missing site name; body:\n%s", aliceHome.Body.String())
	}
	if !strings.Contains(aliceHome.Body.String(), "Alice Only Post") {
		t.Errorf("alice homepage missing her exclusive post")
	}
	if code := get(t, d, "alice.localhost", "/blog/alice-only").Code; code != http.StatusOK {
		t.Errorf("alice /blog/alice-only = %d, want 200", code)
	}

	// Bob must NOT see Alice's post — data isolation.
	bobHome := get(t, d, "bob.localhost", "/")
	if strings.Contains(bobHome.Body.String(), "Alice Only Post") {
		t.Errorf("bob homepage leaked Alice's post")
	}
	if code := get(t, d, "bob.localhost", "/blog/alice-only").Code; code != http.StatusNotFound {
		t.Errorf("bob /blog/alice-only = %d, want 404 (isolation)", code)
	}

	// Unknown subdomain → 404.
	if code := get(t, d, "nope.localhost", "/").Code; code != http.StatusNotFound {
		t.Errorf("unknown subdomain = %d, want 404", code)
	}
}

// TestDispatcherDestroyEvicts asserts that destroying a site that is currently
// cached (its handler built, its DB open) stops it serving immediately and
// removes it from disk — the bug where a live handler kept a destroyed site
// alive and held its files open.
func TestDispatcherDestroyEvicts(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Provision("zeta", "Zeta"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	d := NewDispatcher(reg, "localhost", 0)
	defer d.Close()

	// Serve once so the handler is built and the DB is held open in the cache.
	if code := get(t, d, "zeta.localhost", "/").Code; code != http.StatusOK {
		t.Fatalf("zeta pre-destroy = %d, want 200", code)
	}

	// Destroy through the dispatcher: must evict, close the DB, and remove files.
	if err := d.DestroySite("zeta"); err != nil {
		t.Fatalf("DestroySite (files held open by cached handler?): %v", err)
	}
	if _, ok := reg.Dir("zeta"); ok {
		t.Error("site folder still present after destroy")
	}
	if code := get(t, d, "zeta.localhost", "/").Code; code != http.StatusNotFound {
		t.Errorf("zeta after destroy = %d, want 404 (no stale-cache serving)", code)
	}
}

// TestDispatcherPanicRecovery asserts that a panic in one site's handler yields a
// 500 for that request and leaves the network serving — the honest cost of
// in-process tenancy, paid down with per-request recovery.
func TestDispatcherPanicRecovery(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// A real site (so the dispatcher's self-heal check passes) whose cached
	// handler we replace with one that panics — a stand-in for a tenant bug.
	if _, err := reg.Provision("boom", "Boom"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	d := NewDispatcher(reg, "localhost", 0)
	d.cache["boom"] = &siteHandler{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("tenant went boom")
	})}
	d.order = append(d.order, "boom")

	if code := get(t, d, "boom.localhost", "/").Code; code != http.StatusInternalServerError {
		t.Fatalf("panicking site = %d, want 500", code)
	}
	// The network is still alive: the apex still serves.
	if code := get(t, d, "localhost", "/").Code; code != http.StatusOK {
		t.Errorf("apex after panic = %d, want 200 (network survived)", code)
	}
}
