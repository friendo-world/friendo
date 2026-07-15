package admin

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/api"
	"github.com/friendo-world/friendo/runtime/go/data"
)

//go:embed spa
var spaFiles embed.FS

// spaFS is the built admin SPA (index.html + assets/), rooted at the spa dir.
var spaFS, _ = fs.Sub(spaFiles, "spa")

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips password authentication.
var openAdminMode bool

// Mount registers the admin UI under /_/ on the given router:
//   - /_/api/*       the REST + sync API (see the api package)
//   - /_/assets/*    the built SPA assets
//   - /_/*           the SPA app shell (client-side routing, incl. first-run setup)
//
// If openAdmin is true, the admin UI skips password authentication.
func Mount(r chi.Router, db *data.DB, openAdmin bool, name, siteDir string, permalink data.PermalinkFunc) {
	openAdminMode = openAdmin

	authFunc := func(req *http.Request) *data.User { return GetSessionUser(req, db) }

	r.Route("/_", func(r chi.Router) {
		// REST + sync API.
		api.Mount(r, db, siteDir, name, authFunc, permalink)

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
	if openAdminMode {
		// In open admin mode, return a fake owner (all capabilities).
		return &data.User{
			ID:    "open-admin",
			Name:  "Admin (open mode)",
			Email: "admin@localhost",
			Role:  "owner",
		}
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	user, err := db.ValidateSession(cookie.Value)
	if err != nil {
		return nil
	}
	return user
}
