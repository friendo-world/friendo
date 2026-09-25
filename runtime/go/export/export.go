package export

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/flosch/pongo2/v6"

	"github.com/friendo-world/friendo/runtime/go/calendar"
	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/renderer" // also registers filters + gate tags
)

// exportConfig is the slice of friendo.toml a static export cares about: the
// site name, and the [access] rules that mark whole paths members-only.
type exportConfig struct {
	Site struct {
		Name string `toml:"name"`
	} `toml:"site"`
	Access renderer.AccessRules `toml:"access"`
	// [deploy] gives the calendar feed an absolute URL to link back with.
	Deploy struct {
		Target string `toml:"target"`
		Domain string `toml:"domain"`
	} `toml:"deploy"`
}

// baseURL is where the exported site will live, if friendo.toml says.
func (c exportConfig) baseURL() string {
	if c.Deploy.Domain != "" {
		return "https://" + strings.TrimSuffix(c.Deploy.Domain, "/")
	}
	return strings.TrimSuffix(c.Deploy.Target, "/")
}

func loadExportConfig(siteDir string) exportConfig {
	cfg := exportConfig{}
	if tomlPath := filepath.Join(siteDir, "friendo.toml"); fileExists(tomlPath) {
		toml.DecodeFile(tomlPath, &cfg)
	}
	if cfg.Site.Name == "" {
		cfg.Site.Name = filepath.Base(siteDir)
	}
	return cfg
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Run exports the site in the given mode.
// "static" renders all pages to dist/ as flat HTML files.
// "bundle" is planned but not yet implemented.
func Run(mode string) error {
	switch mode {
	case "static":
		return exportStatic()
	case "bundle":
		return exportBundle()
	default:
		return fmt.Errorf("unknown export mode %q (use static or bundle)", mode)
	}
}

func exportStatic() error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	pagesDir := filepath.Join(siteDir, "pages")
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		return fmt.Errorf("pages directory not found")
	}

	distDir := filepath.Join(siteDir, "dist")
	if err := os.RemoveAll(distDir); err != nil {
		return fmt.Errorf("cleaning dist: %w", err)
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("creating dist: %w", err)
	}

	// Copy assets/ to dist/assets/
	assetsDir := filepath.Join(siteDir, "assets")
	if _, err := os.Stat(assetsDir); err == nil {
		if err := copyDir(assetsDir, filepath.Join(distDir, "assets")); err != nil {
			return fmt.Errorf("copying assets: %w", err)
		}
	}

	// Open database for collection data.
	db, err := data.Open(siteDir)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	loader := pongo2.MustNewLocalFileSystemLoader(siteDir)
	tplSet := pongo2.NewSet("friendo", loader)
	cfg := loadExportConfig(siteDir)
	site := map[string]string{"name": cfg.Site.Name, "url": cfg.baseURL()}
	links := calendar.Links(cfg.baseURL())

	// Build collections context.
	collections := make(map[string]any)
	names, _ := db.ListCollections()
	for _, name := range names {
		// Export only published posts — drafts/pending must not leak into a
		// public static site (matches the serve + edge render paths).
		records, err := db.QueryPublishedCollection(name)
		if err != nil {
			continue
		}
		db.AttachWhen(records, time.Now(), false) // {{ e.when }} on listings
		db.AttachLocations(records)
		collections[name] = records
	}

	count := 0
	err = filepath.Walk(pagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		rel, _ := filepath.Rel(pagesDir, path)
		rel = filepath.ToSlash(rel)

		urlPath := "/" + strings.TrimSuffix(rel, ".html")
		if strings.HasSuffix(urlPath, "/index") {
			urlPath = strings.TrimSuffix(urlPath, "index")
		}

		// A members-only page has no place in a static export: a static host
		// serves everything as 200 to everyone, and nobody is signed in. Skip it
		// (not a stub — a stub would still be indexed) and say so.
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if renderer.FindGate(src) != "" || cfg.Access.Requires(urlPath) != "" {
			fmt.Printf("Skipped %s (members only)\n", rel)
			return nil
		}

		// Skip dynamic route templates — they need to be rendered per-record.
		if strings.Contains(rel, "[") {
			return renderDynamicPages(tplSet, db, collections, site, rel, distDir, &count)
		}

		ctx := pongo2.Context{
			"site":        site,
			"request":     map[string]string{"path": urlPath},
			"collections": collections,
			"calendar":    links,
		}

		tpl, err := tplSet.FromFile("pages/" + rel)
		if err != nil {
			return fmt.Errorf("loading template %s: %w", rel, err)
		}

		out, err := tpl.Execute(ctx)
		if err != nil {
			return fmt.Errorf("rendering %s: %w", rel, err)
		}

		// Write to dist. index.html -> dist/index.html, about.html -> dist/about/index.html
		var outPath string
		if rel == "index.html" || rel == "404.html" {
			outPath = filepath.Join(distDir, rel)
		} else {
			name := strings.TrimSuffix(rel, ".html")
			outPath = filepath.Join(distDir, name, "index.html")
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
			return err
		}

		count++
		return nil
	})

	if err != nil {
		return err
	}

	// The calendar feeds, so a static site is subscribable and <friendo-calendar>
	// has its data with no runtime behind it.
	if n, err := writeFeeds(db, pagesDir, distDir, cfg); err != nil {
		return fmt.Errorf("writing calendar feeds: %w", err)
	} else if n > 0 {
		fmt.Printf("Exported calendar.ics + calendar.json (%d events)\n", n)
	}

	fmt.Printf("Exported %d pages to dist/\n", count)
	return nil
}

// writeFeeds writes dist/calendar.ics and dist/calendar.json from the published
// events, leaving out collections whose page is members-only, exactly as the
// running site does. Returns the number of events written (0 = no feed files).
func writeFeeds(db *data.DB, pagesDir, distDir string, cfg exportConfig) (int, error) {
	// Which collections have a public page, and where it lives.
	pagePath := map[string]string{} // collection -> URL template with [slug]
	gated := map[string]bool{}
	filepath.Walk(pagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.Contains(info.Name(), "[") || !strings.HasSuffix(path, ".html") {
			return nil
		}
		rel, _ := filepath.Rel(pagesDir, path)
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return nil
		}
		collection := parts[len(parts)-2]
		urlPath := "/" + strings.TrimSuffix(rel, ".html")
		src, _ := os.ReadFile(path)
		if renderer.FindGate(src) != "" || cfg.Access.Requires(urlPath) != "" {
			gated[collection] = true
		}
		if _, seen := pagePath[collection]; !seen {
			pagePath[collection] = urlPath
		}
		return nil
	})
	visible := func(c string) bool { return !gated[c] }
	permalink := func(collection string, fields map[string]string) string {
		tpl, ok := pagePath[collection]
		if !ok {
			return ""
		}
		return strings.ReplaceAll(tpl, "[slug]", fields["slug"])
	}
	entries, err := calendar.Collect(db, visible, permalink, calendar.Filter{})
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	ics, err := os.Create(filepath.Join(distDir, "calendar.ics"))
	if err != nil {
		return 0, err
	}
	defer ics.Close()
	if err := calendar.WriteICS(ics, entries, calendar.ICSOptions{SiteName: cfg.Site.Name, SiteZone: db.Location, BaseURL: cfg.baseURL()}); err != nil {
		return 0, err
	}
	// A static file can't take a window, so it carries the most a request could ask for.
	from := time.Now().In(db.Location)
	to := from.AddDate(2, 0, 0)
	payload := map[string]any{
		"site": cfg.Site.Name, "timezone": db.Location.String(),
		"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339),
		"events": calendar.Occurrences(entries, from, to, cfg.baseURL()),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, err
	}
	return len(entries), os.WriteFile(filepath.Join(distDir, "calendar.json"), b, 0o644)
}

// renderDynamicPages renders a dynamic route template (e.g. blog/[slug].html)
// once per matching record and writes each to dist.
func renderDynamicPages(tplSet *pongo2.TemplateSet, db *data.DB, collections map[string]any, site map[string]string, rel, distDir string, count *int) error {
	// Extract collection name from parent directory.
	// e.g. "blog/[slug].html" -> collection "blog"
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return nil
	}
	collectionName := parts[len(parts)-2]

	// Only published posts get a generated page (no draft/pending leak).
	records, err := db.QueryPublishedCollection(collectionName)
	if err != nil || len(records) == 0 {
		return nil
	}
	db.AttachWhen(records, time.Now(), false) // {{ record.when }} on the page
	db.AttachLocations(records)

	tpl, err := tplSet.FromFile("pages/" + rel)
	if err != nil {
		return fmt.Errorf("loading template %s: %w", rel, err)
	}

	for _, record := range records {
		slug, _ := record["slug"].(string)
		if slug == "" {
			continue
		}

		urlPath := "/" + collectionName + "/" + slug

		ctx := pongo2.Context{
			"site":        site,
			"request":     map[string]string{"path": urlPath},
			"collections": collections,
			"record":      record,
			"calendar":    calendar.Links(site["url"]),
		}

		out, err := tpl.Execute(ctx)
		if err != nil {
			fmt.Printf("Warning: failed to render %s for slug %q: %v\n", rel, slug, err)
			continue
		}

		outPath := filepath.Join(distDir, collectionName, slug, "index.html")
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
			return err
		}

		*count++
	}

	return nil
}

// exportBundle packages the site directory and the friendo binary into a .tar.gz archive.
func exportBundle() error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Resolve the running binary so it can be included in the bundle.
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating friendo binary: %w", err)
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	siteName := filepath.Base(siteDir)
	outPath := filepath.Join(siteDir, siteName+".tar.gz")

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating archive: %w", err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Directories to include in the bundle (content/ carries file-authored posts;
	// data/ is added separately as a scrubbed copy below).
	dirs := []string{"pages", "layouts", "assets", "content"}
	for _, dir := range dirs {
		dirPath := filepath.Join(siteDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}
		if err := addDirToTar(tw, dirPath, dir); err != nil {
			return fmt.Errorf("adding %s to archive: %w", dir, err)
		}
	}

	// Add the database as a scrubbed copy — transient/secret auth material (live
	// sessions, one-time codes, rate-limit state) must never travel in a bundle.
	if err := addScrubbedDB(tw, siteDir); err != nil {
		return fmt.Errorf("bundling database: %w", err)
	}

	// Include friendo.toml if it exists.
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		if err := addFileToTar(tw, tomlPath, "friendo.toml"); err != nil {
			return fmt.Errorf("adding friendo.toml to archive: %w", err)
		}
	}

	// Include the friendo binary.
	if err := addFileToTar(tw, binaryPath, "friendo"); err != nil {
		return fmt.Errorf("adding binary to archive: %w", err)
	}

	fmt.Printf("Bundled site to %s\n", outPath)
	return nil
}

// addScrubbedDB copies the site database, deletes transient/secret auth material
// (sessions, one-time codes, rate-limit state), and adds the copy to the archive.
// Content, users (bcrypt hashes are portable by design), and settings are kept.
func addScrubbedDB(tw *tar.Writer, siteDir string) error {
	src := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(src); err != nil {
		return nil // no database to bundle
	}

	tmp, err := os.MkdirTemp("", "friendo-bundle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "data"), 0o755); err != nil {
		return err
	}
	dst := filepath.Join(tmp, "data", "friendo.db")
	if err := copyFile(src, dst); err != nil {
		return err
	}

	db, err := data.Open(tmp)
	if err != nil {
		return err
	}
	for _, table := range []string{"sessions", "otp_codes", "rate_limits"} {
		db.Conn.Exec("DELETE FROM " + table)
	}
	db.Conn.Exec("VACUUM")
	db.Close()

	return addFileToTar(tw, dst, "data/friendo.db")
}

// copyFile copies a single file's contents.
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// addFileToTar adds a single file to the tar writer under the given archive name.
func addFileToTar(tw *tar.Writer, filePath, archiveName string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = archiveName

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(tw, f)
	return err
}

// addDirToTar recursively adds a directory to the tar writer under the given prefix.
func addDirToTar(tw *tar.Writer, srcDir, prefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(srcDir, path)
		archiveName := filepath.ToSlash(filepath.Join(prefix, rel))

		if info.IsDir() {
			header := &tar.Header{
				Name:     archiveName + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			}
			return tw.WriteHeader(header)
		}

		return addFileToTar(tw, path, archiveName)
	})
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
