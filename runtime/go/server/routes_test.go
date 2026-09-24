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
