package server

import (
	"bytes"
	"fmt"
	htmltpl "html/template"
	"io"
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

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/calendar"
	"github.com/friendo-world/friendo/runtime/go/content"
	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/renderer" // also registers filters + gate tags
	"github.com/friendo-world/friendo/runtime/go/sdk"
	"github.com/friendo-world/friendo/runtime/go/storage"
)

// siteConfig represents the friendo.toml file.
type siteConfig struct {
	Site struct {
		Name string `toml:"name"`
	} `toml:"site"`
	// [access] — members-only paths, checked before routing (see renderer.AccessRules).
	Access renderer.AccessRules `toml:"access"`
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
// If openAdmin is true, the admin UI skips authentication entirely. Otherwise,
// unless requireLogin is set, the admin still opens without a login for requests
// from this machine when no email provider is configured (the everyday local
// dev case — there'd be nowhere to send a sign-in code anyway).
func Start(port int, openAdmin, requireLogin bool) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	admin.LocalOpen(!openAdmin && !requireLogin)
	if !openAdmin && !requireLogin && os.Getenv("RESEND_API_KEY") == "" {
		log.Printf("Admin opens without a sign-in for requests from this machine (use --require-login to test signing in).")
	}

	// Local dev convenience: unless a real email provider is configured (or the
	// operator already chose), show one-time login codes in API responses so
	// community features are testable offline. Warn loudly — this must not be on
	// for a publicly exposed site.
	if os.Getenv("FRIENDO_OTP_ECHO") == "" && os.Getenv("RESEND_API_KEY") == "" {
		os.Setenv("FRIENDO_OTP_ECHO", "1")
		log.Printf("⚠  Dev mode: one-time login codes are returned in API responses. " +
			"Set RESEND_API_KEY (+ FRIENDO_EMAIL_FROM) before exposing this site publicly.")
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

	// Compile the file-based content/ folder into the database (no-op if absent).
	if res, err := content.Import(siteDir, db); err != nil {
		log.Printf("Content import failed: %v", err)
	} else if res != nil {
		log.Printf("Content: %s", res.Summary())
	}

	site, err := BuildSite(siteDir, db, openAdmin)
	if err != nil {
		return err
	}

	// Start file watcher for hot reload. A change under pages/ also rebuilds the
	// route table, so a new [slug].html starts routing without a restart.
	go watchForChanges(siteDir, db, site.Reload)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Serving on http://localhost%s\n", addr)
	fmt.Printf("Admin UI on http://localhost%s/_/\n", addr)
	return http.ListenAndServe(addr, site.Handler)
}

// BuiltSite is a site ready to serve, plus what the network needs to know about
// it beyond the handler: whether the site's own pages answer a given path (so a
// home site can deliberately take over one of the network's default pages), and
// a way to re-read pages/ after templates change.
type BuiltSite struct {
	Handler  http.Handler
	table    *routeTable
	pagesDir string
}

// Reload rebuilds the route table from pages/. Called after a templates push
// and by the dev-server file watcher, so a page added after the site was first
// served — a new dynamic [slug].html, say — starts routing at once. Before this
// existed, a site deployed to a network could 404 on every article until the
// process restarted: `friendo deploy` reaches the site's push API (which builds
// the handler, and its routes) before the templates it's pushing exist on disk.
func (s *BuiltSite) Reload() {
	if err := s.table.Reload(); err != nil {
		log.Printf("Rebuilding routes: %v", err)
	}
}

// HasPage reports whether the site's pages/ folder defines urlPath — either a
// routed page (including dynamic ones) or a direct pages/<path>.html file. It
// mirrors handleTemplate's matching without rendering anything.
func (s *BuiltSite) HasPage(urlPath string) bool {
	if urlPath == "" {
		urlPath = "/"
	}
	for _, r := range s.table.snapshot() {
		if r.pattern.MatchString(urlPath) {
			return true
		}
	}
	direct := filepath.Join(s.pagesDir, filepath.Clean(urlPath)+".html")
	_, err := os.Stat(direct)
	return err == nil
}

// routeTable is the page → URL map, rebuildable at runtime. Reads take a
// snapshot under a read lock, so a rebuild never races a request mid-match.
type routeTable struct {
	pagesDir string
	mu       sync.RWMutex
	routes   []route
}

func newRouteTable(pagesDir string) (*routeTable, error) {
	t := &routeTable{pagesDir: pagesDir}
	return t, t.Reload()
}

// Reload re-walks pages/ and swaps the table in one go.
func (t *routeTable) Reload() error {
	routes, err := buildRoutes(t.pagesDir)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.routes = routes
	t.mu.Unlock()
	return nil
}

func (t *routeTable) snapshot() []route {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.routes
}

// BuildSiteHandler assembles the site-serving HTTP handler: the admin UI + REST/sync
// API at /_/, the community SDK at /friendo.js, static /assets/*, and the catch-all
// template renderer. Start wraps it with a listener and a file watcher; the parity
// tests mount it directly over a temp site + DB, so both drive the exact same
// rendering path. Callers own content import and (for dev) the reload watcher.
func BuildSiteHandler(siteDir string, db *data.DB, openAdmin bool) (http.Handler, error) {
	s, err := BuildSite(siteDir, db, openAdmin)
	if err != nil {
		return nil, err
	}
	return s.Handler, nil
}

// BuildSite is BuildSiteHandler plus the page table (see BuiltSite).
func BuildSite(siteDir string, db *data.DB, openAdmin bool) (*BuiltSite, error) {
	pagesDir := filepath.Join(siteDir, "pages")

	// Load site configuration from friendo.toml (or use directory name).
	siteCfg := loadSiteConfig(siteDir)

	// Create a template set rooted at the site directory. Template paths
	// are relative to the site root, e.g. "pages/index.html" and
	// "layouts/base.html". This means {% extends "layouts/base.html" %}
	// resolves naturally from any page template.
	// Debug mode disables caching so template changes take effect immediately.
	loader := pongo2.MustNewLocalFileSystemLoader(siteDir)
	tplSet := pongo2.NewSet("friendo", loader)
	tplSet.Debug = true

	// Build the route table from the pages directory (live — see routeTable).
	table, err := newRouteTable(pagesDir)
	if err != nil {
		return nil, fmt.Errorf("building routes: %w", err)
	}
	built := &BuiltSite{table: table, pagesDir: pagesDir}

	r := chi.NewRouter()

	// Where managed media lives: nil = local disk (default); an S3/R2 backend
	// when FRIENDO_S3_* is configured. Templates and static assets always stay on
	// disk; managed media (assets/uploads/* + assets/galleries/*) is offloaded.
	store, err := storage.FromEnv(siteDir)
	if err != nil {
		return nil, fmt.Errorf("configuring media storage: %w", err)
	}

	// Admin UI + REST/sync API at /_/ (the admin package mounts the api package).
	// The permalink resolver lets the locations API return each post's public URL
	// so an aggregate <friendo-map> can link markers back to their posts.
	// A templates push re-reads pages/ so new routes serve at once.
	admin.Mount(r, db, openAdmin, siteCfg.Site.Name, siteDir, permalinkResolver(table.snapshot), store, built.Reload)

	// Live reload SSE endpoint.
	r.Get("/_/reload", handleReloadSSE)

	// The community SDK (Web Components) at /friendo.js — public.
	r.Get("/friendo.js", sdk.Handler())

	// Calendar feeds: /calendar.ics (subscribe in a calendar app) and
	// /calendar.json (<friendo-calendar>). Published posts with a `when`, minus
	// any collection whose page is members-only. A page the site defines at the
	// same path wins — the same rule as the network's default pages.
	feed := calendar.Feed{
		DB:        db,
		SiteName:  siteCfg.Site.Name,
		Visible:   collectionVisibility(table.snapshot, siteCfg),
		Permalink: permalinkResolver(table.snapshot),
	}
	feedOrPage := func(serve http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			if built.HasPage(req.URL.Path) {
				handleTemplate(w, req, db, pagesDir, tplSet, table.snapshot(), siteCfg)
				return
			}
			serve(w, req)
		}
	}
	r.Get("/calendar.ics", feedOrPage(feed.ServeICS))
	r.Get("/calendar.json", feedOrPage(feed.ServeJSON))

	// Serve static files from assets/ at /assets/. With a media backend, uploads
	// stream from object storage while static assets still come from disk.
	assetsDir := filepath.Join(siteDir, "assets")
	if store != nil {
		r.Handle("/assets/*", freshAssets(assetHandler(assetsDir, store)))
	} else if _, err := os.Stat(assetsDir); err == nil {
		r.Handle("/assets/*", freshAssets(http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir)))))
	}

	// Catch-all: template rendering.
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		handleTemplate(w, req, db, pagesDir, tplSet, table.snapshot(), siteCfg)
	})

	built.Handler = r
	return built, nil
}

// freshAssets sets the cache policy for /assets/*. A deploy replaces files in
// place under the same names, so by default browsers and CDNs must check back
// with the site on every request ("max-age=0, must-revalidate"): the file
// server answers an unchanged file with a tiny 304, and a changed one shows up
// immediately instead of after a CDN's default TTL (Cloudflare's is four
// hours). A URL that carries a version, like /assets/style.css?v=3, is treated
// as immutable and cached for a year, for sites that want that.
func freshAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}

// assetHandler serves /assets/*: managed media (assets/uploads/* + galleries/*)
// streams from the media backend, while static site assets come from the on-disk
// assets dir.
func assetHandler(assetsDir string, store storage.Backend) http.Handler {
	fileServer := http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/assets/")
		if storage.IsManagedMedia(rel) {
			rc, ct, err := store.Open(r.Context(), "assets/"+rel)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer rc.Close()
			if ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			io.Copy(w, rc)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
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
// a browser reload via SSE. Debounced to avoid rapid-fire reloads. Changes under
// content/ trigger a re-import into the database before the reload.
func watchForChanges(siteDir string, db *data.DB, reloadRoutes func()) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Hot reload disabled: %v", err)
		return
	}
	defer watcher.Close()

	// Watch pages/, templates/, public/, and content/ recursively.
	for _, dir := range []string{"pages", "layouts", "assets", "content"} {
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
				changed := event.Name
				debounce = time.AfterFunc(100*time.Millisecond, func() {
					log.Printf("File changed: %s", changed)
					// A pages/ change may add or remove a route (a new folder, a
					// new [slug].html): rebuild the table so it serves right away.
					if reloadRoutes != nil && strings.Contains(filepath.ToSlash(changed), "/pages/") {
						reloadRoutes()
					}
					// A content/ change means a markdown edit — recompile into the DB
					// (collections are re-queried per request, so the reload picks it up).
					if strings.Contains(filepath.ToSlash(changed), "/content/") {
						if res, err := content.Import(siteDir, db); err != nil {
							log.Printf("Content import failed: %v", err)
						} else if res != nil {
							log.Printf("Content: %s", res.Summary())
						}
					}
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
	urlTemplate    string // literal path with [param] segments, e.g. "/blog/[slug]"
	// gate is the page's {% members only %} tag (if any), compiled on its own so it
	// can run before the page renders; gateErr is a broken tag, reported like any
	// other template error when the page is requested.
	gate    *pongo2.Template
	gateErr error
}

// pageGate reads a page file and compiles its gate tag, if it has one.
func pageGate(filePath string) (*pongo2.Template, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	tag := renderer.FindGate(src)
	if tag == "" {
		return nil, nil
	}
	return renderer.CompileGate(tag)
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

		gate, gateErr := pageGate(path)
		if gateErr != nil {
			log.Printf("Members-only tag in %s: %v", path, gateErr)
		}

		routes = append(routes, route{
			pattern:        compiled,
			filePath:       path,
			params:         params,
			collectionName: collectionName,
			urlTemplate:    urlPath,
			gate:           gate,
			gateErr:        gateErr,
		})

		return nil
	})

	return routes, err
}

// collectionVisibility reports whether a collection may appear in a public feed:
// not when its page carries a members-only tag or sits under an [access] path.
// A collection with no page at all is visible (it's public in collections.* too).
func collectionVisibility(current func() []route, siteCfg siteConfig) func(string) bool {
	return func(collection string) bool {
		for _, r := range current() {
			if r.collectionName != collection {
				continue
			}
			if r.gate != nil || siteCfg.Access.Requires(r.urlTemplate) != "" {
				return false
			}
		}
		return true
	}
}

// permalinkResolver builds a data.PermalinkFunc from the route table — the
// reverse of buildRoutes. Given a collection and a record's field values it
// finds the collection's dynamic page and substitutes the values back into the
// URL template (e.g. blog + {slug: "hello"} -> "/blog/hello"). It returns "" for
// collections with no public page. This is why a link on the aggregate map is
// correct even for nested pages, where a naive "/{collection}/{slug}" is wrong.
func permalinkResolver(current func() []route) data.PermalinkFunc {
	return func(collection string, fields map[string]string) string {
		routes := current()
		for i := range routes {
			r := &routes[i]
			if r.collectionName != collection || r.urlTemplate == "" {
				continue
			}
			url := r.urlTemplate
			for _, p := range r.params {
				url = strings.ReplaceAll(url, "["+p+"]", fields[p])
			}
			return url
		}
		return ""
	}
}

// --- Template rendering ---

// handleTemplate matches the request path to a route, builds the template
// context (including collections and the viewer), checks any members-only gate,
// and renders the response.
func handleTemplate(w http.ResponseWriter, req *http.Request, db *data.DB, pagesDir string, tplSet *pongo2.TemplateSet, routes []route, siteCfg siteConfig) {
	urlPath := req.URL.Path
	if urlPath == "" {
		urlPath = "/"
	}

	// Who's viewing. A real session always wins. The localhost open rule needs
	// the admin SPA's own header, so a plain page load on localhost is a visitor
	// — community features test the way they'll behave for real. (--open-admin,
	// which skips auth entirely, does make every page visitor the owner.)
	user := admin.GetSessionUser(req, db)

	// friendo.toml [access] rules run before routing, so a 404 under a
	// members-only prefix shows the sign-in page too rather than revealing
	// which pages exist.
	if required := siteCfg.Access.Requires(urlPath); required != "" {
		if ge := renderer.CheckRole(viewerContext(user), required); ge != nil {
			renderBlocked(w, req, pagesDir, tplSet, baseContext(req, db, siteCfg, user), ge)
			return
		}
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
			matched.gate, matched.gateErr = pageGate(directPath)
		} else {
			renderNotFound(w, req, db, pagesDir, tplSet, siteCfg, user)
			return
		}
	}

	if matched.gateErr != nil {
		http.Error(w, "Template error: "+matched.gateErr.Error(), 500)
		return
	}

	relPath, _ := filepath.Rel(filepath.Dir(pagesDir), matched.filePath)
	tpl, err := tplSet.FromFile(filepath.ToSlash(relPath))
	if err != nil {
		log.Printf("Template error in %s: %v", matched.filePath, err)
		http.Error(w, "Template error: "+err.Error(), 500)
		return
	}

	ctx := baseContext(req, db, siteCfg, user)

	if matched.collectionName != "" && paramValues != nil {
		for paramName, paramValue := range paramValues {
			record, err := db.QueryCollectionByField(matched.collectionName, paramName, paramValue)
			if err != nil {
				log.Printf("Dynamic route lookup failed: %v", err)
				renderNotFound(w, req, db, pagesDir, tplSet, siteCfg, user)
				return
			}
			// Unpublished posts (draft/pending) aren't visible to the public.
			if s, _ := record["status"].(string); s != "published" {
				renderNotFound(w, req, db, pagesDir, tplSet, siteCfg, user)
				return
			}
			attachRecordRelations(db, record)
			ctx["record"] = record
		}
	}

	// The page's own {% members only %} tag, run against the full context (after
	// the record lookup, so an `if` can look at the record too).
	if matched.gate != nil {
		ge, err := renderer.CheckGate(matched.gate, ctx)
		if err != nil {
			log.Printf("Members-only check in %s: %v", matched.filePath, err)
			http.Error(w, "Template error: "+err.Error(), 500)
			return
		}
		if ge != nil {
			renderBlocked(w, req, pagesDir, tplSet, ctx, ge)
			return
		}
	}

	out, err := tpl.Execute(ctx)
	if err != nil {
		// A gate tag inside a layout or a block fails here instead; nothing has
		// been written yet, so the visitor still gets the sign-in page.
		if ge := renderer.GateErrorFrom(err); ge != nil {
			renderBlocked(w, req, pagesDir, tplSet, ctx, ge)
			return
		}
		log.Printf("Render error in %s: %v", matched.filePath, err)
		http.Error(w, "Render error", 500)
		return
	}

	// Inject live reload script before </body>.
	out = strings.Replace(out, "</body>", liveReloadScript+"\n</body>", 1)

	// Pages render the viewer, so a shared cache must not hand one person's page
	// to another.
	w.Header().Set("Vary", "Cookie")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, out)
}

// baseContext is what every page render starts from: the site, the request, the
// collections, and who's viewing (user is nil for a visitor).
func baseContext(req *http.Request, db *data.DB, siteCfg siteConfig, user *data.User) pongo2.Context {
	origin := requestOrigin(req)
	return pongo2.Context{
		"site":        map[string]string{"name": siteCfg.Site.Name, "url": origin},
		"request":     map[string]string{"path": req.URL.Path},
		"collections": buildCollectionsContext(db),
		"user":        viewerContext(user),
		// {{ calendar.google }} / .webcal / .ics — ways to subscribe to the site's events.
		"calendar": calendar.Links(origin),
	}
}

// requestOrigin is the site's own origin as this request saw it (scheme +
// host), for absolute links; behind a proxy X-Forwarded-Proto tells the scheme.
func requestOrigin(req *http.Request) string {
	if req.Host == "" {
		return ""
	}
	scheme := "http"
	if req.TLS != nil || strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}

// viewerContext is the signed-in account as templates see it: {{ user.name }},
// {{ user.role }} and so on — never the password hash. nil when nobody is signed
// in, so {% if user %} reads naturally.
func viewerContext(u *data.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"id":    u.ID,
		"name":  u.Name,
		"email": u.Email,
		"role":  u.Role,
	}
}

// renderBlocked answers a members-only page the viewer may not see: the same URL,
// status 401 (sign in first) or 403 (signed in, but not enough), showing the site's
// own pages/login.html if it has one, else a small built-in sign-in page. The login
// page gets a `gate` variable ({{ gate.required }}, {{ gate.reason }},
// {{ gate.path }}) so it can explain itself.
func renderBlocked(w http.ResponseWriter, req *http.Request, pagesDir string, tplSet *pongo2.TemplateSet, ctx pongo2.Context, ge *renderer.GateError) {
	status := http.StatusForbidden
	if ge.Reason == "signin" {
		status = http.StatusUnauthorized
	}
	ctx["gate"] = map[string]string{"required": ge.Role, "reason": ge.Reason, "path": req.URL.Path}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")

	if _, err := os.Stat(filepath.Join(pagesDir, "login.html")); err == nil {
		if tpl, err := tplSet.FromFile("pages/login.html"); err != nil {
			log.Printf("Template error in pages/login.html: %v", err)
		} else if out, err := tpl.Execute(ctx); err != nil {
			// A gated login page (or any other error) falls through to the
			// built-in one rather than looping.
			log.Printf("Render error in pages/login.html: %v", err)
		} else {
			w.WriteHeader(status)
			fmt.Fprint(w, strings.Replace(out, "</body>", liveReloadScript+"\n</body>", 1))
			return
		}
	}

	site, _ := ctx["site"].(map[string]string)
	user, _ := ctx["user"].(map[string]any)
	var buf bytes.Buffer
	if err := builtinLoginPage.Execute(&buf, map[string]any{
		"Site":     site["name"],
		"Required": ge.Role,
		"Reason":   ge.Reason,
		"User":     user,
	}); err != nil {
		log.Printf("Built-in sign-in page: %v", err)
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.WriteHeader(status)
	fmt.Fprint(w, strings.Replace(buf.String(), "</body>", liveReloadScript+"\n</body>", 1))
}

// builtinLoginPage is what a members-only page shows when the site has no
// pages/login.html of its own: the site name, one line about why, and
// <friendo-auth reload>, which reloads the page once you're signed in.
var builtinLoginPage = htmltpl.Must(htmltpl.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Sign in · {{.Site}}</title>
<style>
body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.5;color:#222}
h1{font-size:1.4rem;margin:0 0 .25rem}
p{margin:.5rem 0 1.25rem}
a{color:inherit}
</style>
</head>
<body>
<h1>{{.Site}}</h1>
{{if eq .Reason "signin"}}<p>Sign in to see this page.</p>
{{else if eq .Reason "role"}}<p>This page is for {{.Required}}s and up. You're signed in as {{with .User}}{{.name}} ({{.role}}){{end}}.</p>
{{else}}<p>This page isn't available to your account{{with .User}} ({{.email}}){{end}}.</p>
{{end}}
<friendo-auth reload></friendo-auth>
<p><a href="/">← Home</a></p>
<script src="/friendo.js" defer></script>
</body>
</html>
`))

// renderNotFound renders pages/404.html if it exists, otherwise a plain 404.
func renderNotFound(w http.ResponseWriter, req *http.Request, db *data.DB, pagesDir string, tplSet *pongo2.TemplateSet, siteCfg siteConfig, user *data.User) {
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

	out, err := tpl.Execute(baseContext(req, db, siteCfg, user))
	if err != nil {
		log.Printf("Error rendering 404 template: %v", err)
		http.NotFound(w, req)
		return
	}

	out = strings.Replace(out, "</body>", liveReloadScript+"\n</body>", 1)

	w.Header().Set("Vary", "Cookie")
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
		// Public render path: only published posts are visible (drafts/pending stay hidden).
		records, err := db.QueryPublishedCollection(name)
		if err != nil {
			log.Printf("Warning: collection %q not available: %v", name, err)
			result[name] = []map[string]any{}
			continue
		}
		// Every record carries its calendar series as record.when (nil for a post
		// with no time), so a listing can print dates and the upcoming/in_month
		// filters can expand without more lookups — and its pin as record.location.
		db.AttachWhen(records, time.Now(), false)
		db.AttachLocations(records)
		result[name] = records
	}

	return result
}

// attachRecordRelations enriches a single focused post record with the related
// data templates can render server-side without JS: its public community data —
// approved comments ({{ record.comments }}), reaction tallies ({{ record.reactions }}),
// and (if the post declares one in its front matter) its poll ({{ record.poll }}) —
// plus its gallery images ({{ record.gallery }}). These are two spellings of what
// the <friendo-*> SDK components show client-side. Viewer-relative fields (a
// reaction's `reacted`, a poll's `my_vote`) take their no-viewer values here; the
// components own the interactive, signed-in view. The edge runtime mirrors this in
// attachRecordRelations.
func attachRecordRelations(db *data.DB, record map[string]any) {
	id, _ := record["id"].(string)
	if id == "" {
		return
	}

	// A feature that's switched off (Settings → Features) renders as if the
	// post had none of it, so a template's {% if record.comments %} stays quiet.

	// Approved comments, oldest first (always present, possibly empty).
	record["comments"] = []map[string]any{}
	if db.FeatureOn("comments") {
		if comments, err := db.ListCommentsByPost(id, false); err == nil {
			record["comments"] = comments
		}
	}

	// Reaction tallies (no server-side viewer → reacted is false).
	if db.FeatureOn("reactions") {
		if reactions, err := db.ReactionCounts("post", id, ""); err == nil {
			record["reactions"] = reactions
		}
	} else {
		record["reactions"] = []map[string]any{}
	}

	// Poll declared in the post's front matter (data.poll.slug), if any. Resolving
	// lazily creates the poll on first view — matching the SDK/REST path.
	if slug := recordPollSlug(record); slug != "" && db.FeatureOn("polls") {
		if pollID, err := db.ResolvePollBySlug(slug); err == nil {
			if poll, err := db.GetPoll(pollID, ""); err == nil {
				record["poll"] = poll
			}
		}
	}

	// Gallery images imported from the post's page bundle (field="gallery").
	if gallery, err := db.ListFilesByField("post", id, "gallery"); err == nil {
		record["gallery"] = gallery
	}

	// The post's pin ({{ record.location.label }}), nil when it has none.
	if db.FeatureOn("locations") {
		db.AttachLocations([]map[string]any{record})
	} else {
		record["location"] = nil
	}

	// The post's calendar series ({{ record.when }}), nil when it has no time,
	// and the RSVP tally for its next occurrence ({{ record.rsvps.going }}).
	if ev := db.EventFor(id); ev != nil {
		record["when"] = ev.Map(time.Now())
		if db.FeatureOn("rsvp") {
			record["rsvps"] = db.RSVPSummary(ev, time.Now())
		} else {
			record["rsvps"] = nil
		}
	} else {
		record["when"] = nil
		record["rsvps"] = nil
	}
}

// recordPollSlug extracts data.poll.slug from a record, or "" if absent.
func recordPollSlug(record map[string]any) string {
	data, _ := record["data"].(map[string]any)
	poll, _ := data["poll"].(map[string]any)
	slug, _ := poll["slug"].(string)
	return slug
}
