package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/henryholtgeerts/friendo/runtime/go/api"
	"github.com/henryholtgeerts/friendo/runtime/go/data"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed spa
var spaFiles embed.FS

// spaFS is the built admin SPA (index.html + assets/), rooted at the spa dir.
var spaFS, _ = fs.Sub(spaFiles, "spa")

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips password authentication.
var openAdminMode bool

// siteName is the configured site name (the SPA reads it from the settings API).
var siteName string

// funcMap holds the template helpers used by the remaining server-rendered
// pages (setup, migrate).
var funcMap = template.FuncMap{
	"join": strings.Join,
}

// pageNames are the pages still rendered server-side, backed by
// templates/<name>.html. The rest of the admin UI is the SPA.
var pageNames = []string{"setup", "migrate"}

// pageTemplates maps a page name to a parsed template set (base + page).
var pageTemplates = buildTemplates()

func buildTemplates() map[string]*template.Template {
	m := make(map[string]*template.Template, len(pageNames))
	for _, p := range pageNames {
		t := template.New(p).Funcs(funcMap)
		t = template.Must(t.ParseFS(templateFiles, "templates/base.html", "templates/"+p+".html"))
		template.Must(t.New("content").Parse(`{{template "` + p + `" .}}`))
		m[p] = t
	}
	return m
}

// Mount registers the admin UI under /_/ on the given router:
//   - /_/api/*       the REST + sync API (see the api package)
//   - /_/assets/*    the built SPA assets
//   - /_/setup, /_/migrate   first-run flows (still server-rendered)
//   - /_/*           the SPA app shell (client-side routing)
//
// If openAdmin is true, the admin UI skips password authentication.
func Mount(r chi.Router, db *data.DB, openAdmin bool, name, siteDir string) {
	openAdminMode = openAdmin
	siteName = name

	authFunc := func(req *http.Request) *data.User { return GetSessionUser(req, db) }

	r.Route("/_", func(r chi.Router) {
		// REST + sync API.
		api.Mount(r, db, siteDir, authFunc)

		// Tailwind CSS for the server-rendered setup/migrate pages.
		staticFS, _ := fs.Sub(staticFiles, "static")
		r.Handle("/static/*", http.StripPrefix("/_/static/", http.FileServer(http.FS(staticFS))))

		// First-run flows (not yet ported to the SPA).
		r.Get("/setup", handleSetupForm(db))
		r.Post("/setup", handleSetupSubmit(db))
		r.Get("/migrate", handleMigrateForm(db))
		r.Post("/migrate", handleMigrateSubmit(db))

		// SPA bundle: built assets and the app shell.
		r.Handle("/assets/*", http.StripPrefix("/_/", http.FileServer(http.FS(spaFS))))
		r.Get("/*", serveSPA(db))
	})
}

// serveSPA serves the SPA app shell, redirecting to the first-run flows when the
// site has no admin account yet (those pages aren't part of the SPA yet).
func serveSPA(db *data.DB) http.HandlerFunc {
	index, err := fs.ReadFile(spaFiles, "spa/index.html")
	if err != nil {
		log.Printf("admin SPA index.html missing — run `npm run admin` to build it: %v", err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !openAdminMode && !db.IsSetupDone() {
			if db.HasLegacyAdmin() {
				http.Redirect(w, r, "/_/migrate", http.StatusFound)
			} else {
				http.Redirect(w, r, "/_/setup", http.StatusFound)
			}
			return
		}
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
		// In open admin mode, return a fake superadmin.
		return &data.User{
			ID:    "open-admin",
			Name:  "Admin (open mode)",
			Email: "admin@localhost",
			Role:  "superadmin",
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

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/_/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 7,
	})
}

// --- First-run handlers (server-rendered) ---

func handleSetupForm(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db.IsSetupDone() {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}
		if db.HasLegacyAdmin() {
			http.Redirect(w, r, "/_/migrate", http.StatusFound)
			return
		}
		renderPage(w, "setup", nil)
	}
}

func handleSetupSubmit(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db.IsSetupDone() {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}

		email := strings.TrimSpace(r.FormValue("email"))
		name := strings.TrimSpace(r.FormValue("name"))
		password := r.FormValue("password")

		if email == "" {
			renderPage(w, "setup", map[string]any{"Error": "Email is required."})
			return
		}
		if len(password) < 8 {
			renderPage(w, "setup", map[string]any{"Error": "Password must be at least 8 characters.", "Email": email, "Name": name})
			return
		}
		if name == "" {
			name = strings.Split(email, "@")[0]
		}

		user, err := db.CreateUser(email, name, password, "superadmin")
		if err != nil {
			renderPage(w, "setup", map[string]any{"Error": "Failed to create account: " + err.Error(), "Email": email, "Name": name})
			return
		}

		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			renderPage(w, "setup", map[string]any{"Error": "Account created but session failed. Please log in."})
			return
		}

		setSessionCookie(w, token)
		http.Redirect(w, r, "/_/", http.StatusFound)
	}
}

func handleMigrateForm(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.HasLegacyAdmin() {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}
		renderPage(w, "migrate", nil)
	}
}

func handleMigrateSubmit(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.HasLegacyAdmin() {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}

		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			renderPage(w, "migrate", map[string]any{"Error": "Email is required."})
			return
		}

		if err := db.MigrateAdminToUsers(email); err != nil {
			renderPage(w, "migrate", map[string]any{"Error": "Migration failed: " + err.Error()})
			return
		}

		http.Redirect(w, r, "/_/", http.StatusFound)
	}
}

// --- Helpers ---

func renderPage(w http.ResponseWriter, page string, d map[string]any) {
	t, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "Page not found", 404)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", d); err != nil {
		log.Printf("Admin render error: %v", err)
	}
}
