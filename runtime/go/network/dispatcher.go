package network

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/friendo-world/friendo/runtime/go/content"
	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
)

// Dispatcher is the network-mode HTTP handler. It routes each request to the
// right tenant by subdomain and serves it with the exact single-site handler
// that `friendo serve` builds — so managed hosting and self-hosting run byte-for-
// byte the same code path. Site handlers are built lazily and cached; the apex
// (bare base domain) serves a minimal operator view.
//
// Tenancy is in-process and cheap: an idle site costs about a file handle, and
// there is no per-site process or cold-start. The honest cost — blast-radius
// isolation — is handled here with per-request panic recovery; a hot or abusive
// tenant can later be moved to its own process running this same binary.
type Dispatcher struct {
	reg        *Registry
	baseDomain string

	mu        sync.Mutex
	cache     map[string]*siteHandler
	order     []string // LRU: least-recent first, most-recent last
	maxCached int

	// apexHandler serves the bare base domain (the operator console). When nil,
	// the apex falls back to a minimal site listing (serveApex).
	apexHandler http.Handler

	// suspended reports whether a site is on hold, and why. Set by the running
	// network (see SetSuspendedCheck); nil means nothing is suspended, which
	// keeps the dispatcher independent of the accounts store.
	suspended func(sub string) (Suspension, bool)

	// domainLookup resolves a *verified* custom hostname to a site — the fallback
	// when a request host isn't under baseDomain. This is the first time routing
	// is not a pure string operation, so results are cached (see resolveDomain)
	// rather than hitting the store on every request.
	domainLookup func(host string) (string, bool)
	domainMu     sync.Mutex
	domainCache  map[string]domainEntry
}

// domainEntry is one cached host→site answer, including the negative one — an
// unknown host is the common case for stray internet traffic, and re-asking the
// store for every hit of it is the thing worth avoiding.
type domainEntry struct {
	sub   string
	found bool
	at    time.Time
}

// domainCacheTTL bounds how stale a routing answer can be. A domain that is
// removed or unverified keeps routing for at most this long, which is the price
// of not querying per request.
const domainCacheTTL = 30 * time.Second

// maxDomainCache caps the cache. Past it the map is dropped wholesale rather
// than evicted one by one — it refills in a few requests and the bookkeeping
// isn't worth it at this size.
const maxDomainCache = 1024

type siteHandler struct {
	handler http.Handler
	db      *data.DB
}

// NewDispatcher builds a dispatcher over reg. baseDomain is the host suffix under
// which sites are served (`<subdomain>.<baseDomain>`); maxCached bounds how many
// site handlers/DBs are kept open at once (<=0 picks a default).
func NewDispatcher(reg *Registry, baseDomain string, maxCached int) *Dispatcher {
	if maxCached <= 0 {
		maxCached = 64
	}
	return &Dispatcher{
		reg:        reg,
		baseDomain: strings.ToLower(strings.TrimSpace(baseDomain)),
		cache:      map[string]*siteHandler{},
		maxCached:  maxCached,
	}
}

// HandleApex sets the handler for the bare base domain — the operator console.
func (d *Dispatcher) HandleApex(h http.Handler) { d.apexHandler = h }

// SetSuspendedCheck tells the dispatcher how to spot a suspended site — normally
// Accounts.SiteSuspension. A suspended site serves a hold page instead of the
// tenant, without its data being touched or its handler evicted.
func (d *Dispatcher) SetSuspendedCheck(fn func(string) (Suspension, bool)) { d.suspended = fn }

// SetDomainLookup tells the dispatcher how to resolve a custom hostname —
// normally Accounts.SiteForDomain, which only answers for verified domains.
// Without it, a host outside the base domain falls through to the apex as before.
func (d *Dispatcher) SetDomainLookup(fn func(string) (string, bool)) { d.domainLookup = fn }

// resolveDomain answers host→site through a short-lived cache.
func (d *Dispatcher) resolveDomain(host string) (string, bool) {
	d.domainMu.Lock()
	defer d.domainMu.Unlock()
	if e, ok := d.domainCache[host]; ok && time.Since(e.at) < domainCacheTTL {
		return e.sub, e.found
	}
	sub, found := d.domainLookup(host)
	if d.domainCache == nil || len(d.domainCache) >= maxDomainCache {
		d.domainCache = map[string]domainEntry{}
	}
	d.domainCache[host] = domainEntry{sub: sub, found: found, at: time.Now()}
	return sub, found
}

// ForgetDomain drops a cached routing answer so a change lands at once instead
// of after the TTL — used when a domain is disconnected or newly verified.
func (d *Dispatcher) ForgetDomain(host string) {
	d.domainMu.Lock()
	defer d.domainMu.Unlock()
	delete(d.domainCache, strings.ToLower(strings.TrimSpace(host)))
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub := d.subdomain(r.Host)
	// Not under the base domain — it may be a tenant's own domain. Only verified
	// ones resolve, so an unverified claim can never take someone else's traffic.
	if sub == "" && d.domainLookup != nil {
		if host := hostOnly(r.Host); host != "" && host != d.baseDomain {
			if s, ok := d.resolveDomain(host); ok {
				sub = s
			}
		}
	}
	if sub == "" {
		if d.apexHandler != nil {
			d.apexHandler.ServeHTTP(w, r)
			return
		}
		d.serveApex(w, r)
		return
	}

	// A suspended site never reaches its handler — checked before the cache so
	// resuming and suspending both take effect on the next request.
	if d.suspended != nil {
		if s, held := d.suspended(sub); held {
			d.serveHold(w, r, sub, s)
			return
		}
	}

	handler, err := d.siteHandler(sub)
	if err != nil {
		http.Error(w, fmt.Sprintf("No site at %s.%s", sub, d.baseDomain), http.StatusNotFound)
		return
	}

	// Per-request panic recovery: one tenant's panic returns a 500 for that one
	// request and can never take down the process or its neighbours.
	defer func() {
		if v := recover(); v != nil {
			log.Printf("[network] panic serving %s%s: %v", r.Host, r.URL.Path, v)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()

	handler.ServeHTTP(w, r)
}

// subdomain extracts the tenant label from a request host. Returns "" for the
// bare base domain (the apex/operator view) or any host that isn't under it.
func (d *Dispatcher) subdomain(host string) string {
	host = hostOnly(host)
	if host == "" || host == d.baseDomain {
		return ""
	}
	suffix := "." + d.baseDomain
	if !strings.HasSuffix(host, suffix) {
		return "" // unknown host — treat as apex rather than guess a tenant
	}
	label := strings.TrimSuffix(host, suffix)
	if i := strings.IndexByte(label, '.'); i >= 0 {
		label = label[:i] // left-most label only
	}
	return label
}

// hostOnly lowercases a request host and strips any :port.
func hostOnly(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

// siteHandler returns the cached handler for a site, building it (open DB +
// import content + assemble routes) on first use. Cold-start is serialized by
// d.mu for simplicity; a per-site lock is a later optimization.
func (d *Dispatcher) siteHandler(sub string) (http.Handler, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if sh, ok := d.cache[sub]; ok {
		// Self-heal: if the site was removed out-of-band, drop the stale handler
		// rather than serve a deleted site from memory.
		if _, exists := d.reg.Dir(sub); !exists {
			d.dropLocked(sub)
			return nil, fmt.Errorf("no such site %q", sub)
		}
		d.touch(sub)
		return sh.handler, nil
	}

	dir, ok := d.reg.Dir(sub)
	if !ok {
		return nil, fmt.Errorf("no such site %q", sub)
	}

	db, err := data.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("opening site db: %w", err)
	}
	// Compile any content/ markdown into the DB, matching what `friendo serve`
	// does on start (network mode has no file watcher — content arrives via sync).
	if _, err := content.Import(dir, db); err != nil {
		log.Printf("[network] content import failed for %s: %v", sub, err)
	}
	h, err := server.BuildSiteHandler(dir, db, false)
	if err != nil {
		db.Conn.Close()
		return nil, fmt.Errorf("building site handler: %w", err)
	}

	d.cache[sub] = &siteHandler{handler: h, db: db}
	d.order = append(d.order, sub)
	d.trimLocked()
	return h, nil
}

// touch marks sub as most-recently-used. Caller holds d.mu.
func (d *Dispatcher) touch(sub string) {
	for i, s := range d.order {
		if s == sub {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	d.order = append(d.order, sub)
}

// dropLocked closes a single cached site (if present) and removes it from the
// cache and the LRU order. Caller holds d.mu.
func (d *Dispatcher) dropLocked(sub string) {
	if sh, ok := d.cache[sub]; ok {
		if sh.db != nil {
			sh.db.Conn.Close()
		}
		delete(d.cache, sub)
	}
	for i, s := range d.order {
		if s == sub {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}

// trimLocked closes least-recently-used sites until the cache is within bounds.
// Caller holds d.mu.
func (d *Dispatcher) trimLocked() {
	for len(d.order) > d.maxCached {
		d.dropLocked(d.order[0])
	}
}

// DestroySite stops a site serving immediately — evicting its cached handler and
// closing its database — then removes it from disk. The operator console goes
// through here rather than the registry directly, so a destroyed site can't keep
// serving from cache or hold its files open.
func (d *Dispatcher) DestroySite(sub string) error {
	d.mu.Lock()
	d.dropLocked(sub)
	d.mu.Unlock()
	return d.reg.Destroy(sub)
}

// Close releases every cached site's database. Safe to call on shutdown.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for sub, sh := range d.cache {
		if sh.db != nil {
			sh.db.Conn.Close()
		}
		delete(d.cache, sub)
	}
	d.order = nil
}

// ListenAndServe runs the network on the given port until the process stops.
func (d *Dispatcher) ListenAndServe(port int) error {
	addr := fmt.Sprintf(":%d", port)
	log.Printf("friendo network serving on http://localhost%s (base domain %q)", addr, d.baseDomain)
	log.Printf("  apex / operator view:  http://%s%s/", d.baseDomain, addr)
	log.Printf("  a site:                http://<subdomain>.%s%s/", d.baseDomain, addr)
	return http.ListenAndServe(addr, d)
}

// serveHold is what a visitor sees at a suspended site: an explanation, not a
// 404 (the site exists) and not a 503 (nothing is going to fix itself).
func (d *Dispatcher) serveHold(w http.ResponseWriter, r *http.Request, sub string, s Suspension) {
	// Name the address the visitor actually typed. Reached through a tenant's own
	// domain, "zeta.friendo.world" would mean nothing to them.
	shown := hostOnly(r.Host)
	if shown == "" {
		shown = sub + "." + d.baseDomain
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Site on hold</title>`+
		`<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.5;color:#111}`+
		`h1{font-size:1.3rem;margin-bottom:.4rem}.muted{color:#6b7280}</style>`+
		`<h1>This site is on hold</h1>`+
		`<p><strong>%s</strong> has been paused by the operator of this network.</p>`,
		html.EscapeString(shown))
	if s.Reason != "" {
		fmt.Fprintf(w, `<p>Reason: %s</p>`, html.EscapeString(s.Reason))
	}
	fmt.Fprint(w, `<p class="muted">Nothing has been deleted. If this is your site, contact the operator to have it put back.</p>`)
}

// serveApex renders the operator view: a minimal listing of the network's sites.
// This is the placeholder for the full operator console (Phase 2).
func (d *Dispatcher) serveApex(w http.ResponseWriter, r *http.Request) {
	sites, err := d.reg.Sites()
	if err != nil {
		http.Error(w, "network error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>friendo network</title>`+
		`<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem;line-height:1.5}`+
		`h1{margin-bottom:.25rem}.muted{color:#6b7280}li{margin:.35rem 0}</style>`+
		`<h1>friendo network</h1><p class="muted">%d site(s) &middot; operator console (placeholder)</p><ul>`, len(sites))
	for _, s := range sites {
		host := s.Subdomain + "." + d.baseDomain
		fmt.Fprintf(w, `<li><a href="//%s/">%s</a> &mdash; %s</li>`,
			html.EscapeString(host), html.EscapeString(host), html.EscapeString(s.Name))
	}
	if len(sites) == 0 {
		fmt.Fprint(w, `<li class="muted">No sites yet. Create one with <code>friendo network provision &lt;subdomain&gt;</code>.</li>`)
	}
	fmt.Fprint(w, `</ul>`)
}
