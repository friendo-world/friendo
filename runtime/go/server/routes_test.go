package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// The route table can be rebuilt after the site is built, and HasPage follows it.
func TestRouteTableReload(t *testing.T) {
	siteDir := t.TempDir()
	os.MkdirAll(filepath.Join(siteDir, "pages"), 0o755)
	os.WriteFile(filepath.Join(siteDir, "pages", "index.html"), []byte("home"), 0o644)
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	site, err := BuildSite(siteDir, db, false)
	if err != nil {
		t.Fatalf("BuildSite: %v", err)
	}
	if !site.HasPage("/") || site.HasPage("/docs/anything") {
		t.Fatal("initial table wrong")
	}
	os.MkdirAll(filepath.Join(siteDir, "pages", "docs"), 0o755)
	os.WriteFile(filepath.Join(siteDir, "pages", "docs", "[slug].html"), []byte("{{ record.title }}"), 0o644)
	if site.HasPage("/docs/anything") {
		t.Fatal("a new dynamic page should not be routed until Reload")
	}
	site.Reload()
	if !site.HasPage("/docs/anything") {
		t.Fatal("Reload should pick up the new dynamic page")
	}
}

// A page's members-only tag is part of its route, and Reload follows edits to it.
func TestRouteTableGateReload(t *testing.T) {
	siteDir := t.TempDir()
	pages := filepath.Join(siteDir, "pages")
	os.MkdirAll(pages, 0o755)
	secret := filepath.Join(pages, "secret.html")
	os.WriteFile(secret, []byte("public for now"), 0o644)
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	site, err := BuildSite(siteDir, db, false)
	if err != nil {
		t.Fatalf("BuildSite: %v", err)
	}
	gateOf := func() (bool, error) {
		for _, r := range site.table.snapshot() {
			if r.urlTemplate == "/secret" {
				return r.gate != nil, r.gateErr
			}
		}
		t.Fatal("no /secret route")
		return false, nil
	}
	if gated, _ := gateOf(); gated {
		t.Fatal("a page without the tag should not be gated")
	}
	os.WriteFile(secret, []byte("{% extends \"layouts/base.html\" %}\n{% editors only %}\n{% block content %}x{% endblock %}"), 0o644)
	if gated, _ := gateOf(); gated {
		t.Fatal("the gate should not change until Reload")
	}
	site.Reload()
	if gated, err := gateOf(); !gated || err != nil {
		t.Fatalf("Reload should pick up the tag: gated=%v err=%v", gated, err)
	}
	os.WriteFile(secret, []byte("{% editors only if %}"), 0o644)
	site.Reload()
	if _, err := gateOf(); err == nil {
		t.Fatal("a broken tag should be recorded as an error")
	}
}
