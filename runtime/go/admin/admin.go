package admin

import (
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/api"
	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/email"
	"github.com/friendo-world/friendo/runtime/go/storage"
)

//go:embed spa
var spaFiles embed.FS

// spaFS is the built admin SPA (index.html + assets/), rooted at the spa dir.
var spaFS, _ = fs.Sub(spaFiles, "spa")

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips authentication for every request.
var openAdminMode bool

// localOpen is the everyday `friendo serve` convenience: on your own machine,
// with no email provider to deliver a sign-in code, the admin opens without a
// login — but only for requests that are unmistakably local (see isLocalRequest)
// and only from the admin UI itself (it sends adminHeader; the site's own
// <friendo-*> tags don't, so a visitor on localhost is still a visitor and a
// member who signs in is that member). A real session always wins over open
// mode. Set by server.Start alone; a site served by a network never turns it
// on. `friendo serve --require-login` switches it off to test the sign-in screens.
var localOpen bool

// adminHeader is how the admin SPA identifies its own API calls.
const adminHeader = "X-Friendo-Admin"

// LocalOpen turns the localhost convenience on or off.
func LocalOpen(on bool) { localOpen = on }

// Mount registers the admin UI under /_/ on the given router:
//   - /_/api/*       the REST + sync API (see the api package)
//   - /_/assets/*    the built SPA assets
//   - /_/*           the SPA app shell (client-side routing, incl. first-run setup)
//
// If openAdmin is true, the admin UI skips authentication. onTemplatesChanged
// (optional) runs after a templates push, so the server can re-read pages/.
func Mount(r chi.Router, db *data.DB, openAdmin bool, name, siteDir string, permalink data.PermalinkFunc, store storage.Backend, onTemplatesChanged func()) {
	openAdminMode = openAdmin

	authFunc := func(req *http.Request) *data.User { return GetSessionUser(req, db) }

	r.Route("/_", func(r chi.Router) {
		// REST + sync API.
		api.Mount(r, db, siteDir, name, authFunc, permalink, store, onTemplatesChanged)

		// SPA bundle: built assets and the app shell.
		r.Handle("/assets/*", http.StripPrefix("/_/", http.FileServer(http.FS(spaFS))))
		r.Get("/*", serveSPA())
	})
}

// serveSPA serves the SPA app shell. First-run setup, login, and all admin
// routing happen inside the SPA against the REST API.
func serveSPA() http.HandlerFunc {
	index, err := fs.ReadFile(spaFiles, "spa/index.html")
	if err != nil {
		log.Printf("admin SPA index.html missing — run `npm run admin` to build it: %v", err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	}
}

// --- Auth helpers ---

const sessionCookieName = "friendo_session"

// GetSessionUser validates the session cookie and returns the current user.
// Returns nil if not authenticated. Exported for use by the API.
func GetSessionUser(r *http.Request, db *data.DB) *data.User {
	// A real session always wins — a member who signed in on localhost is that
	// member, not the open-mode owner.
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if user, err := db.ValidateSession(cookie.Value); err == nil {
			return user
		}
	}
	if openAdminMode || (localOpen && r.Header.Get(adminHeader) != "" && isLocalRequest(r) && !email.Configured()) {
		// Open mode: act as an owner (all capabilities) without a session.
		return &data.User{
			ID:    "open-admin",
			Name:  "Admin (open mode)",
			Email: "admin@localhost",
			Role:  "owner",
		}
	}
	return nil
}

// isLocalRequest reports whether a request could only have come from the same
// machine: the peer is a loopback address, the Host header names localhost, and
// no proxy header is present. A reverse proxy on the same box fails the Host
// check (it forwards the public name) and normally adds a Forwarded header too,
// so a site exposed through one never opens up by accident.
func isLocalRequest(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Proto") != "" ||
		r.Header.Get("X-Forwarded-Host") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	peer := r.RemoteAddr
	if h, _, err := net.SplitHostPort(peer); err == nil {
		peer = h
	}
	ip := net.ParseIP(strings.Trim(peer, "[]"))
	return ip != nil && ip.IsLoopback()
}
