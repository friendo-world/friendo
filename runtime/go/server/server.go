package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/flosch/pongo2/v6"
	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi/v5"

	"github.com/henryholtgeerts/friendo/runtime/go/admin"
	"github.com/henryholtgeerts/friendo/runtime/go/api"
	"github.com/henryholtgeerts/friendo/runtime/go/data"
	_ "github.com/henryholtgeerts/friendo/runtime/go/renderer" // registers filters
)

// siteConfig represents the friendo.toml file.
type siteConfig struct {
	Site struct {
		Name string `toml:"name"`
	} `toml:"site"`
}

func loadSiteConfig(siteDir string) siteConfig {
	cfg := siteConfig{}
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		toml.DecodeFile(tomlPath, &cfg)
	}
	if cfg.Site.Name == "" {
		cfg.Site.Name = filepath.Base(siteDir)
	}
	return cfg
}

// dynamicSegmentRe matches directory or file names like [slug].
var dynamicSegmentRe = regexp.MustCompile(`\[(\w+)\]`)

// liveReload manages SSE connections for hot reload.
var liveReload = &reloadBroadcaster{
	clients: make(map[chan struct{}]struct{}),
}

// Start opens the database, builds routes, and starts the HTTP server.
// If openAdmin is true, the admin UI skips password authentication.
func Start(port int, openAdmin bool) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	pagesDir := filepath.Join(siteDir, "pages")
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		return fmt.Errorf("pages directory not found")
	}

	db, err := data.Open(siteDir)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Load site configuration from friendo.toml (or use directory name).
	siteCfg := loadSiteConfig(siteDir)

	// Create a template set rooted at the site directory. Template paths
	// are relative to the site root, e.g. "pages/index.html" and
	// "templates/base.html". This means {% extends "templates/base.html" %}
	// resolves naturally from any page template.
	// Debug mode disables caching so template changes take effect immediately.
	loader := pongo2.MustNewLocalFileSystemLoader(siteDir)
	tplSet := pongo2.NewSet("friendo", loader)
	tplSet.Debug = true

	// Build the route table from the pages directory.
	routes, err := buildRoutes(pagesDir)
	if err != nil {
		return fmt.Errorf("building routes: %w", err)
	}

	// Start file watcher for hot reload.
	go watchForChanges(siteDir)

	r := chi.NewRouter()

	// Admin UI at /_/
	admin.Mount(r, db, openAdmin, siteCfg.Site.Name)

	// Sync API at /_/api/
	api.Mount(r, db, siteDir, func(req *http.Request) *data.User {
		return admin.GetSessionUser(req, db)
	})

	// Live reload SSE endpoint.
	r.Get("/_/reload", handleReloadSSE)

	// Serve static assets from public/.
	publicDir := filepath.Join(siteDir, "public")
	if _, err := os.Stat(publicDir); err == nil {
		r.Handle("/public/*", http.StripPrefix("/public/", http.FileServer(http.Dir(publicDir))))
	}

	// Catch-all: template rendering.
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		handleTemplate(w, req, db, pagesDir, tplSet, routes, siteCfg)
	})

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Serving on http://localhost%s\n", addr)
	fmt.Printf("Admin UI on http://localhost%s/_/\n", addr)
	return http.ListenAndServe(addr, r)
}

// --- Hot reload ---

// reloadBroadcaster sends reload signals to connected browsers via SSE.
type reloadBroadcaster struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
}

func (b *reloadBroadcaster) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *reloadBroadcaster) unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *reloadBroadcaster) notify() {
	b.mu.Lock()
	for ch := range b.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	b.mu.Unlock()
}

func handleReloadSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := liveReload.subscribe()
	defer liveReload.unsubscribe(ch)

	for {
		select {
		case <-ch:
			fmt.Fprintf(w, "data: reload\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// watchForChanges watches the site directory for file changes and triggers
// a browser reload via SSE. Debounced to avoid rapid-fire reloads.
func watchForChanges(siteDir string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Hot reload disabled: %v", err)
		return
	}
	defer watcher.Close()

	// Watch pages/, templates/, and public/ recursively.
	for _, dir := range []string{"pages", "templates", "public"} {
		dirPath := filepath.Join(siteDir, dir)
		if _, err := os.Stat(dirPath); err != nil {
			continue
		}
		filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				watcher.Add(path)
			}
			return nil
		})
	}

	var debounce *time.Timer
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) != 0 {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(100*time.Millisecond, func() {
					log.Printf("File changed: %s", event.Name)
					liveReload.notify()
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// liveReloadScript is injected before </body> in rendered templates.
const liveReloadScript = `<script>
(function(){var es=new EventSource("/_/reload");es.onmessage=function(){location.reload()};es.onerror=function(){es.close();setTimeout(function(){location.reload()},1000)}})();
</script>`

// --- Routing ---

// route represents a single page template and how it maps to URL paths.
type route struct {
	pattern        *regexp.Regexp
	filePath       string
	params         []string
	collectionName string
}

// buildRoutes walks the pages directory and creates a route table.
func buildRoutes(pagesDir string) ([]route, error) {
	var routes []route

	err := filepath.Walk(pagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		rel, _ := filepath.Rel(pagesDir, path)
		rel = filepath.ToSlash(rel)

		// Skip special files.
		if rel == "404.html" {
			return nil
		}

		urlPath := "/" + strings.TrimSuffix(rel, ".html")
		if strings.HasSuffix(urlPath, "/index") {
			urlPath = strings.TrimSuffix(urlPath, "index")
		}

		var params []string
		collectionName := ""
		regexPath := urlPath

		if dynamicSegmentRe.MatchString(urlPath) {
			parts := strings.Split(strings.TrimPrefix(urlPath, "/"), "/")
			if len(parts) >= 2 {
				collectionName = parts[len(parts)-2]
			}

			regexPath = dynamicSegmentRe.ReplaceAllStringFunc(urlPath, func(match string) string {
				paramName := dynamicSegmentRe.FindStringSubmatch(match)[1]
				params = append(params, paramName)
				return `(?P<` + paramName + `>[^/]+)`
			})
		}

		if regexPath == "/" {
			regexPath = `^/?$`
		} else {
			regexPath = `^` + regexPath + `$`
		}

		compiled, err := regexp.Compile(regexPath)
		if err != nil {
			return fmt.Errorf("compiling route pattern %q: %w", regexPath, err)
		}

		routes = append(routes, route{
			pattern:        compiled,
			filePath:       path,
			params:         params,
			collectionName: collectionName,
		})

		return nil
	})

	return routes, err
}

// --- Template rendering ---

// handleTemplate matches the request path to a route, builds the template
// context (including collections), and renders the response.
func handleTemplate(w http.ResponseWriter, req *http.Request, db *data.DB, pagesDir string, tplSet *pongo2.TemplateSet, routes []route, siteCfg siteConfig) {
	urlPath := req.URL.Path
	if urlPath == "" {
		urlPath = "/"
	}

	var matched *route
	var paramValues map[string]string

	for i := range routes {
		r := &routes[i]
		if r.pattern.MatchString(urlPath) {
			matched = r

			if len(r.params) > 0 {
				matches := r.pattern.FindStringSubmatch(urlPath)
				paramValues = make(map[string]string)
				for j, name := range r.pattern.SubexpNames() {
					if j > 0 && name != "" {
						paramValues[name] = matches[j]
					}
				}
			}
			break
		}
	}

	if matched == nil {
		directPath := filepath.Join(pagesDir, filepath.Clean(urlPath)+".html")
		if _, err := os.Stat(directPath); err == nil {
			matched = &route{filePath: directPath}
		} else {
			renderNotFound(w, req, db, pagesDir, tplSet, siteCfg)
			return
		}
	}

	relPath, _ := filepath.Rel(filepath.Dir(pagesDir), matched.filePath)
	tpl, err := tplSet.FromFile(filepath.ToSlash(relPath))
	if err != nil {
		log.Printf("Template error in %s: %v", matched.filePath, err)
		http.Error(w, "Template error: "+err.Error(), 500)
		return
	}

	ctx := pongo2.Context{
		"site":    map[string]string{"name": siteCfg.Site.Name},
		"request": map[string]string{"path": req.URL.Path},
	}

	ctx["collections"] = buildCollectionsContext(db)

	if matched.collectionName != "" && paramValues != nil {
		for paramName, paramValue := range paramValues {
			record, err := db.QueryCollectionByField(matched.collectionName, paramName, paramValue)
			if err != nil {
				log.Printf("Dynamic route lookup failed: %v", err)
				renderNotFound(w, req, db, pagesDir, tplSet, siteCfg)
				return
			}
			ctx["record"] = record
		}
	}

	out, err := tpl.Execute(ctx)
	if err != nil {
		log.Printf("Render error in %s: %v", matched.filePath, err)
		http.Error(w, "Render error", 500)
		return
	}

	// Inject live reload script before </body>.
	out = strings.Replace(out, "</body>", liveReloadScript+"\n</body>", 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, out)
}

// renderNotFound renders pages/404.html if it exists, otherwise a plain 404.
func renderNotFound(w http.ResponseWriter, req *http.Request, db *data.DB, pagesDir string, tplSet *pongo2.TemplateSet, siteCfg siteConfig) {
	notFoundPath := filepath.Join(pagesDir, "404.html")
	if _, err := os.Stat(notFoundPath); err != nil {
		http.NotFound(w, req)
		return
	}

	tpl, err := tplSet.FromFile("pages/404.html")
	if err != nil {
		log.Printf("Error loading 404 template: %v", err)
		http.NotFound(w, req)
		return
	}

	ctx := pongo2.Context{
		"site":        map[string]string{"name": "Friendo Site"},
		"request":     map[string]string{"path": req.URL.Path},
		"collections": buildCollectionsContext(db),
	}

	out, err := tpl.Execute(ctx)
	if err != nil {
		log.Printf("Error rendering 404 template: %v", err)
		http.NotFound(w, req)
		return
	}

	out = strings.Replace(out, "</body>", liveReloadScript+"\n</body>", 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(404)
	fmt.Fprint(w, out)
}

// buildCollectionsContext queries all collections and returns them as a
// map[string]any for Pongo2 template access.
func buildCollectionsContext(db *data.DB) map[string]any {
	result := make(map[string]any)

	names, err := db.ListCollections()
	if err != nil {
		log.Printf("Warning: could not list collections: %v", err)
		return result
	}

	for _, name := range names {
		records, err := db.QueryCollection(name)
		if err != nil {
			log.Printf("Warning: collection %q not available: %v", name, err)
			result[name] = []map[string]any{}
			continue
		}
		result[name] = records
	}

	return result
}
