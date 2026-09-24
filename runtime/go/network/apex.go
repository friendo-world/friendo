package network

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/sdk"
)

// The network's own pages on the apex. Each is a thin shell around one
// <friendo-*> tag from the SDK, so the same tag dropped into a home site's own
// page gives the branded version — and a home site that defines one of these
// paths itself takes it over (see Dispatcher.ServeHTTP). /api/* is always the
// network's, because the tags talk to it.
var defaultPages = map[string]struct{ title, tag string }{
	"/login":    {"Sign in", "<friendo-account></friendo-account>"},
	"/account":  {"Your sites", "<friendo-account></friendo-account>"},
	"/network":  {"Network console", "<friendo-console></friendo-console>"},
	"/activate": {"Link this device", `<friendo-activate code="%s"></friendo-activate>`},
}

// DefaultPage reports whether a path is one of the network's own pages.
func DefaultPage(path string) bool {
	_, ok := defaultPages[strings.TrimSuffix(path, "/")]
	return ok || path == "/"
}

// ApexRouter is the handler for the network's bare domain when the home site
// doesn't answer: identity + account + operator APIs, the default pages, the
// SDK they need, and a landing page for a network with no home site yet.
func ApexRouter(aa *AccountAuth, op *OperatorAPI) http.Handler {
	m := http.NewServeMux()
	aa.register(m)
	op.register(m)
	m.HandleFunc("GET /friendo.js", sdk.Handler())
	for path, page := range defaultPages {
		path, page := path, page
		m.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			tag := page.tag
			if strings.Contains(tag, "%s") {
				tag = fmt.Sprintf(tag, html.EscapeString(strings.ToUpper(r.URL.Query().Get("code"))))
			}
			serveShell(w, page.title, aa.base, tag)
		})
		m.HandleFunc("GET "+path+"/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, path, http.StatusMovedPermanently)
		})
	}
	m.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { serveLanding(w, aa.base) })
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such endpoint"})
			return
		}
		serveNotFound(w, aa.base)
	})
	return m
}

const shellCSS = `body{font-family:system-ui,sans-serif;max-width:44rem;margin:3rem auto;padding:0 1rem;line-height:1.5;color:#111}` +
	`h1{font-size:1.4rem;margin:0 0 .25rem}.muted{color:#6b7280}nav{margin-bottom:1.5rem;font-size:.9rem}` +
	`nav a{color:inherit;margin-right:1rem}code{background:#f3f4f6;padding:.1rem .3rem;border-radius:.2rem}` +
	`@media(prefers-color-scheme:dark){body{background:#111;color:#eee}.muted{color:#9ca3af}code{background:#222}}`

// serveShell renders one default page: a heading, the network's nav, and the
// component that does the work.
func serveShell(w http.ResponseWriter, title, base, tag string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1">`+
		`<title>%s · %s</title><style>%s</style>`+
		`<script src="/friendo.js" defer></script></head><body>`+
		`<nav><a href="/">%s</a><a href="/account">Your sites</a><a href="/network">Network console</a></nav>`+
		`<h1>%s</h1>%s</body></html>`,
		html.EscapeString(title), html.EscapeString(base), shellCSS,
		html.EscapeString(base), html.EscapeString(title), tag)
}

// serveLanding is what the bare domain shows until the operator picks a home
// site: a sentence about what this is, and the two doors in. It deliberately
// lists nothing — the tenant list isn't public.
func serveLanding(w http.ResponseWriter, base string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1">`+
		`<title>%s</title><style>%s</style></head><body>`+
		`<h1>%s</h1><p class="muted">This is a friendo network — one server hosting many sites, each at its own address.</p>`+
		`<p><a href="/account">Sign in</a> to see your sites, or publish a folder from your computer:</p>`+
		`<p><code>friendo deploy my-site --network https://%s</code></p>`+
		`<p class="muted">Running this network? Sign in and pick a <a href="/network">home site</a> to show here instead of this page.</p>`+
		`</body></html>`,
		html.EscapeString(base), shellCSS, html.EscapeString(base), html.EscapeString(base))
}

func serveNotFound(w http.ResponseWriter, base string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Not found · %s</title><style>%s</style>`+
		`<h1>Nothing here</h1><p class="muted">There's no page at this address on %s. <a href="/">Back to the start.</a></p>`,
		html.EscapeString(base), shellCSS, html.EscapeString(base))
}
