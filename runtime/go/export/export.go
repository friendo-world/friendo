package export

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flosch/pongo2/v6"

	"github.com/friendo-world/friendo/runtime/go/data"
	_ "github.com/friendo-world/friendo/runtime/go/renderer"
)

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

	// Copy public/ assets to dist/public/
	publicDir := filepath.Join(siteDir, "public")
	if _, err := os.Stat(publicDir); err == nil {
		if err := copyDir(publicDir, filepath.Join(distDir, "public")); err != nil {
			return fmt.Errorf("copying public assets: %w", err)
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

	// Build collections context.
	collections := make(map[string]any)
	names, _ := db.ListCollections()
	for _, name := range names {
		records, err := db.QueryCollection(name)
		if err != nil {
			continue
		}
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

		// Skip dynamic route templates — they need to be rendered per-record.
		if strings.Contains(rel, "[") {
			return renderDynamicPages(tplSet, db, collections, rel, distDir, &count)
		}

		urlPath := "/" + strings.TrimSuffix(rel, ".html")
		if strings.HasSuffix(urlPath, "/index") {
			urlPath = strings.TrimSuffix(urlPath, "index")
		}

		ctx := pongo2.Context{
			"site":        map[string]string{"name": "Friendo Site"},
			"request":     map[string]string{"path": urlPath},
			"collections": collections,
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

	fmt.Printf("Exported %d pages to dist/\n", count)
	return nil
}

// renderDynamicPages renders a dynamic route template (e.g. blog/[slug].html)
// once per matching record and writes each to dist.
func renderDynamicPages(tplSet *pongo2.TemplateSet, db *data.DB, collections map[string]any, rel, distDir string, count *int) error {
	// Extract collection name from parent directory.
	// e.g. "blog/[slug].html" -> collection "blog"
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return nil
	}
	collectionName := parts[len(parts)-2]

	records, err := db.QueryCollection(collectionName)
	if err != nil || len(records) == 0 {
		return nil
	}

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
			"site":        map[string]string{"name": "Friendo Site"},
			"request":     map[string]string{"path": urlPath},
			"collections": collections,
			"record":      record,
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

	// Directories to include in the bundle.
	dirs := []string{"pages", "templates", "public", "data"}
	for _, dir := range dirs {
		dirPath := filepath.Join(siteDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}
		if err := addDirToTar(tw, dirPath, dir); err != nil {
			return fmt.Errorf("adding %s to archive: %w", dir, err)
		}
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
