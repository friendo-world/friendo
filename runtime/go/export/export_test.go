package export

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// A static export leaves members-only pages out — tagged pages, tagged dynamic
// pages, and paths named in friendo.toml's [access] — and uses the site's name.
func TestStaticExportSkipsMembersOnlyPages(t *testing.T) {
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"Exported\"\n\n[access]\nmembers_only = [\"/private/*\"]\n")
	write("pages/index.html", "home of {{ site.name }}")
	write("pages/members.html", "{% members only %}secret")
	write("pages/private/index.html", "also secret")
	write("pages/blog/[slug].html", "{% members only %}{{ record.title }}")
	write("pages/notes/[slug].html", "{{ record.title }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"blog", "notes"} {
		if _, err := db.CreateRecord(c, "hello", "Hello", "body", "published", ""); err != nil {
			t.Fatalf("create %s record: %v", c, err)
		}
	}
	db.Close()

	t.Chdir(siteDir)
	if err := Run("static"); err != nil {
		t.Fatalf("export: %v", err)
	}
	dist := filepath.Join(siteDir, "dist")
	home, err := os.ReadFile(filepath.Join(dist, "index.html"))
	if err != nil || string(home) != "home of Exported" {
		t.Fatalf("index.html = %q, %v", home, err)
	}
	if _, err := os.Stat(filepath.Join(dist, "notes", "hello", "index.html")); err != nil {
		t.Fatalf("public dynamic page missing: %v", err)
	}
	for _, gated := range []string{"members/index.html", "private/index.html", "blog/hello/index.html"} {
		if _, err := os.Stat(filepath.Join(dist, gated)); err == nil {
			t.Errorf("%s was exported; members-only pages must be skipped", gated)
		}
	}
}
