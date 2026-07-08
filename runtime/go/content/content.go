// Package content compiles a site's file-based content/ directory into its
// database — Friendo's Hugo-style authoring layer. Markdown files organized in
// folders become records: the folder under content/ is the collection, the
// filename (or page-bundle folder) is the slug, YAML front matter supplies title,
// slug, status, and date, and any remaining front-matter keys are stored as the
// record's `data` JSON (readable in templates as record.data.<field>). The body
// is stored verbatim and rendered by the `markdown` filter at template time.
//
// content/ is the source of truth: importing upserts by (collection, slug), so
// re-running syncs edits. It leaves dynamic content (comments, reactions) alone.
package content

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// defaultCollection holds top-level content/*.md files (those not under a section).
const defaultCollection = "pages"

// Result summarizes an import run.
type Result struct {
	Imported     int
	ByCollection map[string]int
	Warnings     []string
}

func (r *Result) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// HasContent reports whether the site has a content/ directory to import.
func HasContent(siteDir string) bool {
	info, err := os.Stat(filepath.Join(siteDir, "content"))
	return err == nil && info.IsDir()
}

// Build opens the site database, imports content/, and closes it. Convenience for
// the CLI (the `build` command, and push/deploy) where no DB is already open.
// Returns nil,nil when the site has no content/ directory.
func Build(siteDir string) (*Result, error) {
	if !HasContent(siteDir) {
		return nil, nil
	}
	db, err := data.Open(siteDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return Import(siteDir, db)
}

// Summary is a one-line human summary of an import run.
func (r *Result) Summary() string {
	if r == nil {
		return "no content/ directory"
	}
	parts := make([]string, 0, len(r.ByCollection))
	for c, n := range r.ByCollection {
		parts = append(parts, fmt.Sprintf("%s: %d", c, n))
	}
	sort.Strings(parts)
	s := fmt.Sprintf("imported %d record(s)", r.Imported)
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	if len(r.Warnings) > 0 {
		s += fmt.Sprintf("; %d warning(s)", len(r.Warnings))
	}
	return s
}

// Import compiles siteDir/content/ into the database. Returns nil,nil when the
// site has no content/ directory. A single bad file becomes a warning, not a
// fatal error, so one typo doesn't block the rest of the build.
func Import(siteDir string, db *data.DB) (*Result, error) {
	contentDir := filepath.Join(siteDir, "content")
	if !HasContent(siteDir) {
		return nil, nil
	}

	res := &Result{ByCollection: map[string]int{}}

	var files []string
	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking content/: %w", err)
	}
	sort.Strings(files)

	for _, path := range files {
		rel, _ := filepath.Rel(contentDir, path)
		collection, slug := routeFor(rel)

		raw, err := os.ReadFile(path)
		if err != nil {
			res.warn("%s: %v", rel, err)
			continue
		}
		meta, body := parseFrontmatter(raw)

		title := stringField(meta, "title")
		if title == "" {
			title = slug
		}
		if s := stringField(meta, "slug"); s != "" {
			slug = s
		}
		status := stringField(meta, "status")
		if status == "" {
			status = "published"
		}
		publishedAt := stringField(meta, "date")

		// Everything not mapped to a column becomes the record's `data` JSON.
		for _, k := range []string{"title", "slug", "status", "date"} {
			delete(meta, k)
		}
		dataJSON := "{}"
		if len(meta) > 0 {
			if b, err := json.Marshal(meta); err == nil {
				dataJSON = string(b)
			}
		}

		if _, err := db.UpsertRecordBySlug(collection, slug, title, body, status, publishedAt, dataJSON); err != nil {
			res.warn("%s → %s/%s: %v", rel, collection, slug, err)
			continue
		}
		res.Imported++
		res.ByCollection[collection]++
	}

	return res, nil
}

// routeFor maps a content-relative path to (collection, slug):
//
//	content/about.md                  → pages / about        (top-level → default collection)
//	content/docs/installation.md      → docs  / installation
//	content/blog/my-trip/index.md     → blog  / my-trip      (page bundle: slug is the folder)
//
// The collection is always the first path segment; deeper nesting flattens to the
// filename as slug (Friendo slugs can't contain "/").
func routeFor(rel string) (collection, slug string) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	base := parts[len(parts)-1]
	name := strings.TrimSuffix(base, filepath.Ext(base))

	if len(parts) == 1 {
		return defaultCollection, name
	}
	collection = parts[0]
	if name == "index" {
		slug = parts[len(parts)-2] // page bundle: the folder name is the slug
	} else {
		slug = name
	}
	return collection, slug
}

// parseFrontmatter splits a leading `--- ... ---` YAML block from the markdown
// body. Missing/invalid front matter yields an empty map and the whole content.
func parseFrontmatter(raw []byte) (map[string]any, string) {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	meta := map[string]any{}
	if !strings.HasPrefix(s, "---\n") {
		return meta, s
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return meta, s
	}
	block := s[4 : 4+end]
	body := strings.TrimPrefix(s[4+end+4:], "\n")

	if err := yaml.Unmarshal([]byte(block), &meta); err != nil || meta == nil {
		meta = map[string]any{}
	}
	return meta, strings.TrimLeft(body, "\n")
}

// stringField coerces a front-matter value to a string (YAML may decode dates as
// time.Time and numbers as ints).
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case time.Time:
		return t.UTC().Format("2006-01-02T15:04:05Z")
	default:
		return fmt.Sprintf("%v", t)
	}
}
