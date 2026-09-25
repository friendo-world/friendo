// Package content compiles a site's file-based content/ directory into its
// database — Friendo's Hugo-style authoring layer. Markdown files organized in
// folders become records: the folder under content/ is the collection, the
// filename (or page-bundle folder) is the slug, YAML front matter supplies title,
// slug, status, and date; `location` becomes a map pin and `when` (with
// ends/timezone/repeats/except) a calendar event; any remaining front-matter keys
// are stored as the record's `data` JSON (readable in templates as
// record.data.<field>). The body
// is stored verbatim and rendered by the `markdown` filter at template time.
//
// content/ is the source of truth: importing upserts by (collection, slug), so
// re-running syncs edits. It leaves dynamic content (comments, reactions) alone.
package content

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

		// A `location` front-matter field geo-tags the post so it shows on maps
		// (its own <friendo-map> and the aggregate one). Pulled out of `meta` so it
		// becomes a real location row, not opaque `data` JSON.
		lat, lng, label, hasLocation := parseLocation(meta, title)

		// A `when` (plus ends/timezone/repeats/except) makes the post an event: the
		// reserved keys are lifted into a real events row the same way `location`
		// becomes a pin — see data.ParseWhen for the spellings.
		ev, whenWarnings := data.LiftWhen(meta, db.Location)
		for _, w := range whenWarnings {
			res.warn("%s: %s", rel, w)
		}

		// Everything not mapped to a column becomes the record's `data` JSON.
		for _, k := range []string{"title", "slug", "status", "date", "location"} {
			delete(meta, k)
		}
		dataJSON := "{}"
		if len(meta) > 0 {
			if b, err := json.Marshal(meta); err == nil {
				dataJSON = string(b)
			}
		}

		recordID, err := db.UpsertRecordBySlug(collection, slug, title, body, status, publishedAt, dataJSON)
		if err != nil {
			res.warn("%s → %s/%s: %v", rel, collection, slug, err)
			continue
		}
		res.Imported++
		res.ByCollection[collection]++

		// Reconcile the post's content-declared pin: upsert when present, clear when
		// removed. Uses a deterministic id so API-created pins are never touched.
		locID := data.DeterministicLocationID(recordID)
		if hasLocation {
			if err := db.UpsertLocation(locID, "post", recordID, lat, lng, label); err != nil {
				res.warn("%s location: %v", rel, err)
			}
		} else if err := db.DeleteLocation(locID); err != nil && err != sql.ErrNoRows {
			res.warn("%s location: %v", rel, err)
		}

		// Reconcile the post's `when` the same way: upsert when declared, clear
		// when removed (deterministic id, so re-import is idempotent).
		if err := db.ReconcileWhen(recordID, ev); err != nil {
			res.warn("%s when: %v", rel, err)
		}

		// Page bundle (content/<collection>/<slug>/index.md): sibling images become
		// the post's gallery (record.gallery), attached as field="gallery" files.
		if strings.EqualFold(filepath.Base(path), "index.md") {
			importGallery(siteDir, db, res, collection, slug, recordID, filepath.Dir(path))
		}
	}

	return res, nil
}

// galleryExts are the image extensions a page bundle contributes to record.gallery.
var galleryExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// importGallery copies a page bundle's sibling images into the site's assets/ tree
// (so the existing static handler serves them and push syncs them) and attaches a
// field="gallery" file row per image to the post. It reconciles: the managed asset
// dir and the post's gallery rows are cleared and rebuilt from the current folder,
// so removing an image from the bundle removes it on the next build. Ids are
// deterministic, so re-import and re-push upsert in place. Per-image failures are
// warnings, not fatal.
func importGallery(siteDir string, db *data.DB, res *Result, collection, slug, recordID, bundleDir string) {
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		res.warn("%s/%s gallery: %v", collection, slug, err)
		return
	}

	// Collect images in a stable order so `created` ordering is deterministic.
	var images []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := galleryExts[strings.ToLower(filepath.Ext(e.Name()))]; ok {
			images = append(images, e.Name())
		}
	}
	sort.Strings(images)

	// Reconcile: clear the managed asset dir and this post's gallery rows, then
	// rebuild from the current folder.
	assetDir := filepath.Join("assets", "galleries", collection, slug)
	destDir := filepath.Join(siteDir, assetDir)
	os.RemoveAll(destDir)
	if err := db.DeleteFilesByField("post", recordID, "gallery"); err != nil {
		res.warn("%s/%s gallery: %v", collection, slug, err)
		return
	}
	if len(images) == 0 {
		return
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		res.warn("%s/%s gallery: %v", collection, slug, err)
		return
	}

	for _, name := range images {
		size, err := copyFile(filepath.Join(bundleDir, name), filepath.Join(destDir, name))
		if err != nil {
			res.warn("%s/%s gallery %s: %v", collection, slug, name, err)
			continue
		}
		r2Key := filepath.ToSlash(filepath.Join(assetDir, name))
		mime := galleryExts[strings.ToLower(filepath.Ext(name))]
		id := data.DeterministicFileID(recordID, name)
		if err := db.UpsertFile(id, "post", recordID, "gallery", r2Key, mime, size, ""); err != nil {
			res.warn("%s/%s gallery %s: %v", collection, slug, name, err)
		}
	}
}

// copyFile copies src to dst, returning the number of bytes written.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
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

// parseLocation reads a post's `location` front-matter into lat/lng/label. Two
// author-friendly forms are accepted:
//
//	location: "48.8584, 2.2945"           # a "lat, lng" string
//	location: { lat: 48.8584, lng: 2.2945, label: "Eiffel Tower" }
//
// The label defaults to the post title. Returns ok=false when there's no usable
// location (missing field, or coordinates that don't parse / are out of range).
func parseLocation(meta map[string]any, title string) (lat, lng float64, label string, ok bool) {
	v, present := meta["location"]
	if !present {
		return 0, 0, "", false
	}
	label = title
	switch t := v.(type) {
	case string:
		parts := strings.Split(t, ",")
		if len(parts) != 2 {
			return 0, 0, "", false
		}
		var okLat, okLng bool
		lat, okLat = toFloat(strings.TrimSpace(parts[0]))
		lng, okLng = toFloat(strings.TrimSpace(parts[1]))
		if !okLat || !okLng {
			return 0, 0, "", false
		}
	case map[string]any:
		var okLat, okLng bool
		lat, okLat = toFloat(t["lat"])
		lng, okLng = toFloat(t["lng"])
		if !okLat || !okLng {
			return 0, 0, "", false
		}
		if l, isStr := t["label"].(string); isStr && l != "" {
			label = l
		}
	default:
		return 0, 0, "", false
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, "", false
	}
	return lat, lng, label, true
}

// toFloat coerces a YAML scalar (float64, int, or numeric string) to a float64.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
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
