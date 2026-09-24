// Package network is friendo's operator mode: one process that hosts many sites
// by subdomain, reusing the exact single-site runtime that `friendo serve` runs.
// A "network" is a set of sites one operator runs; friendo.world is just the
// reference network. See design: the single-runtime pivot.
package network

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/scaffold"
)

// Registry is the set of sites that make up a network. For this first cut the
// filesystem IS the registry: each immediate subdirectory of Root is a site
// whose folder name is its subdomain ("your site is a folder", one level up).
// Per-site metadata — owner, quotas, suspended state, custom domains — lives in
// the Accounts store (account.go), keyed by subdomain; the registry only knows
// whether a site exists and where its folder is.
type Registry struct {
	Root string
}

// Site is one tenant on the network.
type Site struct {
	Subdomain string `json:"subdomain"`
	Dir       string `json:"-"`    // server-side path; never exposed to API clients
	Name      string `json:"name"` // display name from friendo.toml (falls back to the subdomain)
}

// NewRegistry opens (creating if needed) the network root directory.
func NewRegistry(root string) (*Registry, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("creating network root %q: %w", root, err)
	}
	return &Registry{Root: root}, nil
}

// subdomainRe is the guard for a folder/URL label: a DNS-ish name with no path
// tricks, so a request host can never escape the network root.
var subdomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidSubdomain reports whether s is a usable subdomain/site name.
func ValidSubdomain(s string) bool { return subdomainRe.MatchString(s) }

// ReservedSubdomains are names a self-service account can't claim: the ones
// the network itself uses or might (www, api, the console), and the ones
// people expect to belong to whoever runs the domain (docs, mail, cdn). An
// operator can still provision them — the network's own sites live here.
var ReservedSubdomains = map[string]bool{
	"www": true, "api": true, "admin": true, "mail": true, "origin": true,
	"network": true, "console": true, "account": true, "login": true,
	"activate": true, "docs": true, "static": true, "cdn": true,
}

// IsReserved reports whether a subdomain is kept for the network's own use.
func IsReserved(s string) bool { return ReservedSubdomains[strings.ToLower(strings.TrimSpace(s))] }

// Dir resolves a subdomain to its site directory, reporting false if the
// subdomain is invalid or no site folder is present. A valid site has a pages/
// directory (what the runtime routes from).
func (r *Registry) Dir(subdomain string) (string, bool) {
	if !ValidSubdomain(subdomain) {
		return "", false
	}
	dir := filepath.Join(r.Root, subdomain)
	if fi, err := os.Stat(filepath.Join(dir, "pages")); err != nil || !fi.IsDir() {
		return "", false
	}
	return dir, true
}

// Sites lists every site on the network, sorted by subdomain.
func (r *Registry) Sites() ([]Site, error) {
	entries, err := os.ReadDir(r.Root)
	if err != nil {
		return nil, fmt.Errorf("reading network root: %w", err)
	}
	var sites []Site
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := e.Name()
		dir, ok := r.Dir(sub)
		if !ok {
			continue // not a valid site folder — skip quietly
		}
		sites = append(sites, Site{Subdomain: sub, Dir: dir, Name: siteName(dir, sub)})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Subdomain < sites[j].Subdomain })
	return sites, nil
}

// Provision creates a new site: scaffold the folder, set its display name, and
// initialise its database (schema + migrations) so it serves immediately. The
// Go equivalent of the old Cloudflare-REST provisioner — a folder op, not an API
// dance. Errors if the subdomain is invalid or already taken.
func (r *Registry) Provision(subdomain, displayName string) (Site, error) {
	if !ValidSubdomain(subdomain) {
		return Site{}, fmt.Errorf("invalid subdomain %q — use lowercase letters, digits and hyphens", subdomain)
	}
	dir := filepath.Join(r.Root, subdomain)
	if _, err := os.Stat(dir); err == nil {
		return Site{}, fmt.Errorf("site %q already exists", subdomain)
	}
	if displayName == "" {
		displayName = subdomain
	}

	// scaffold.Init writes the canonical folder plus a friendo.toml whose name is
	// the path we pass; rewrite that to the intended display name.
	if err := scaffold.Init(dir); err != nil {
		return Site{}, fmt.Errorf("scaffolding site: %w", err)
	}
	if err := setSiteName(dir, displayName); err != nil {
		return Site{}, fmt.Errorf("setting site name: %w", err)
	}

	// Initialise the DB now (schema + migrations) so the first request is fast and
	// the site is complete on disk. Opened and immediately closed here; the
	// dispatcher opens its own handle on first request.
	db, err := data.Open(dir)
	if err != nil {
		return Site{}, fmt.Errorf("initialising site database: %w", err)
	}
	db.Conn.Close()

	return Site{Subdomain: subdomain, Dir: dir, Name: displayName}, nil
}

// Destroy removes a site and all its data. Irreversible.
func (r *Registry) Destroy(subdomain string) error {
	dir, ok := r.Dir(subdomain)
	if !ok {
		return fmt.Errorf("no such site %q", subdomain)
	}
	return os.RemoveAll(dir)
}

// siteName reads the display name from a site's friendo.toml, falling back to
// the subdomain when absent.
func siteName(dir, fallback string) string {
	var cfg struct {
		Site struct {
			Name string `toml:"name"`
		} `toml:"site"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "friendo.toml"), &cfg); err == nil && cfg.Site.Name != "" {
		return cfg.Site.Name
	}
	return fallback
}

// tomlNameRe matches the first `name = "..."` assignment (the [site] name that
// scaffold writes) so we can rewrite it without a full TOML round-trip.
var tomlNameRe = regexp.MustCompile(`(?m)^(\s*name\s*=\s*)"[^"]*"`)

// setSiteName rewrites the [site] name in a scaffolded friendo.toml.
func setSiteName(dir, name string) error {
	name = strings.ReplaceAll(name, `"`, `'`) // keep the TOML string well-formed
	p := filepath.Join(dir, "friendo.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	out := tomlNameRe.ReplaceAll(b, []byte(`${1}"`+name+`"`))
	return os.WriteFile(p, out, 0o644)
}
