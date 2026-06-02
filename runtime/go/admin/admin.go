package admin

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/henryholtgeerts/friendo/runtime/go/data"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*.html
var templateFiles embed.FS

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips password authentication.
var openAdminMode bool

// siteName is the configured site name, shown on the settings page.
var siteName string

// funcMap holds the template helpers shared by every admin page.
var funcMap = template.FuncMap{
	"json": func(v any) string {
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	},
	"join":    strings.Join,
	"roleTag": func(role string) template.HTML { return template.HTML(roleTag(role)) },
}

// pageNames are the admin pages backed by templates/<name>.html.
var pageNames = []string{
	"setup", "migrate", "login",
	"dashboard", "collection", "record_form",
	"users", "user_form", "settings",
}

// pageTemplates maps a page name to a parsed template set (base + nav + page),
// built once at startup. Each page renders by executing the "base" template,
// which pulls in the page via a per-page "content" bridge.
var pageTemplates = buildTemplates()

func buildTemplates() map[string]*template.Template {
	m := make(map[string]*template.Template, len(pageNames))
	for _, p := range pageNames {
		t := template.New(p).Funcs(funcMap)
		t = template.Must(t.ParseFS(templateFiles,
			"templates/base.html", "templates/nav.html", "templates/"+p+".html"))
		template.Must(t.New("content").Parse(`{{template "` + p + `" .}}`))
		m[p] = t
	}
	return m
}

// Mount registers the admin UI routes under /_/ on the given router.
// If openAdmin is true, the admin UI is accessible without a password.
func Mount(r chi.Router, db *data.DB, openAdmin bool, name string) {
	openAdminMode = openAdmin
	siteName = name
	r.Route("/_", func(r chi.Router) {
		// Serve embedded static files (CSS).
		staticFS, _ := fs.Sub(staticFiles, "static")
		r.Handle("/static/*", http.StripPrefix("/_/static/", http.FileServer(http.FS(staticFS))))

		// Public routes (no auth required).
		r.Get("/setup", handleSetupForm(db))
		r.Post("/setup", handleSetupSubmit(db))
		r.Get("/migrate", handleMigrateForm(db))
		r.Post("/migrate", handleMigrateSubmit(db))
		r.Get("/login", handleLoginForm(db))
		r.Post("/login", handleLoginSubmit(db))
		r.Post("/logout", handleLogout(db))

		// Protected routes (auth required).
		r.Get("/", handleDashboard(db))
		r.Get("/collections/{collection}", handleCollectionList(db))
		r.Get("/collections/{collection}/new", handleRecordForm(db, false))
		r.Post("/collections/{collection}/new", handleRecordCreate(db))
		r.Get("/collections/{collection}/{id}/edit", handleRecordForm(db, true))
		r.Post("/collections/{collection}/{id}/edit", handleRecordUpdate(db))
		r.Post("/collections/{collection}/{id}/delete", handleRecordDelete(db))

		r.Get("/settings", handleSettings(db))

		// User management (admin+ only).
		r.Get("/users", handleUserList(db))
		r.Get("/users/new", handleUserForm(db, false))
		r.Post("/users/new", handleUserCreate(db))
		r.Get("/users/{id}/edit", handleUserForm(db, true))
		r.Post("/users/{id}/edit", handleUserUpdate(db))
		r.Post("/users/{id}/delete", handleUserDelete(db))
	})
}

// --- Auth helpers ---

const sessionCookieName = "friendo_session"

// GetSessionUser validates the session cookie and returns the current user.
// Returns nil if not authenticated. Exported for use by the sync API.
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

// requireAuth redirects to login if not authenticated. Returns the user or nil.
func requireAuth(w http.ResponseWriter, r *http.Request, db *data.DB) *data.User {
	user := GetSessionUser(r, db)
	if user == nil {
		http.Redirect(w, r, "/_/login", http.StatusFound)
		return nil
	}
	return user
}

// requireRole checks that the user has at least the given role.
func requireRole(w http.ResponseWriter, user *data.User, minRole string) bool {
	if data.RoleRank(user.Role) < data.RoleRank(minRole) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
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

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/_/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// --- Auth handlers ---

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

		http.Redirect(w, r, "/_/login", http.StatusFound)
	}
}

func handleLoginForm(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.IsSetupDone() {
			http.Redirect(w, r, "/_/setup", http.StatusFound)
			return
		}
		renderPage(w, "login", nil)
	}
}

func handleLoginSubmit(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.FormValue("email"))
		password := r.FormValue("password")

		user, err := db.AuthenticateUser(email, password)
		if err != nil {
			renderPage(w, "login", map[string]any{"Error": "Invalid email or password.", "Email": email})
			return
		}

		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			renderPage(w, "login", map[string]any{"Error": "Failed to create session.", "Email": email})
			return
		}

		setSessionCookie(w, token)
		http.Redirect(w, r, "/_/", http.StatusFound)
	}
}

func handleLogout(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			db.DeleteSession(cookie.Value)
		}
		clearSessionCookie(w)
		http.Redirect(w, r, "/_/login", http.StatusFound)
	}
}

// --- Dashboard ---

func handleDashboard(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !openAdminMode && !db.IsSetupDone() {
			if db.HasLegacyAdmin() {
				http.Redirect(w, r, "/_/migrate", http.StatusFound)
			} else {
				http.Redirect(w, r, "/_/setup", http.StatusFound)
			}
			return
		}

		user := requireAuth(w, r, db)
		if user == nil {
			return
		}

		renderPage(w, "dashboard", map[string]any{
			"User":        user,
			"Active":      "dashboard",
			"Collections": collectionStats(db),
		})
	}
}

// CollectionStat is a content type with its record count, shown on the dashboard.
type CollectionStat struct {
	Name  string
	Count int
}

// collectionStats returns each built-in content type with its current record count.
func collectionStats(db *data.DB) []CollectionStat {
	stats := make([]CollectionStat, 0, len(contentTypes))
	for _, ct := range contentTypes {
		records, err := db.QueryCollection(ct)
		if err != nil {
			log.Printf("Error counting collection %q: %v", ct, err)
		}
		stats = append(stats, CollectionStat{Name: ct, Count: len(records)})
	}
	return stats
}

// --- Settings ---

func handleSettings(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}

		collections, _ := db.ListCollections()
		users, err := db.ListUsers()
		if err != nil {
			users = []*data.User{}
		}

		renderPage(w, "settings", map[string]any{
			"User":        user,
			"Active":      "settings",
			"SiteName":    siteName,
			"Collections": collections,
			"UserCount":   len(users),
		})
	}
}

// --- Collection handlers ---

func handleCollectionList(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}

		collection := chi.URLParam(r, "collection")
		records, err := db.QueryCollection(collection)
		if err != nil {
			log.Printf("Error querying collection %q: %v", collection, err)
			records = []map[string]any{}
		}

		renderPage(w, "collection", map[string]any{
			"User":       user,
			"Active":     "",
			"Collection": collection,
			"Records":    records,
		})
	}
}

func handleRecordForm(db *data.DB, editing bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}

		collection := chi.URLParam(r, "collection")
		d := map[string]any{
			"User":       user,
			"Active":     "",
			"Collection": collection,
			"Editing":    editing,
			"Record":     map[string]any{},
		}

		if editing {
			id := chi.URLParam(r, "id")
			record, err := db.QueryCollectionByField(collection, "id", id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			d["Record"] = record
		}

		renderPage(w, "record_form", d)
	}
}

func handleRecordCreate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "editor") {
			return
		}

		collection := chi.URLParam(r, "collection")
		id := data.GenerateID()
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

		_, err := db.Conn.Exec(
			`INSERT INTO posts (id, site_id, collection, slug, title, body, status, author_id, created, updated)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, db.SiteID, collection,
			r.FormValue("slug"), r.FormValue("title"), r.FormValue("body"),
			r.FormValue("status"), user.ID, now, now,
		)
		if err != nil {
			log.Printf("Error creating record: %v", err)
			renderPage(w, "record_form", map[string]any{
				"User":       user,
				"Collection": collection,
				"Editing":    false,
				"Error":      "Failed to create record.",
			})
			return
		}

		http.Redirect(w, r, "/_/collections/"+collection, http.StatusFound)
	}
}

func handleRecordUpdate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "editor") {
			return
		}

		collection := chi.URLParam(r, "collection")
		id := chi.URLParam(r, "id")
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

		_, err := db.Conn.Exec(
			`UPDATE posts SET slug = ?, title = ?, body = ?, status = ?, updated = ?
			 WHERE id = ? AND site_id = ? AND collection = ?`,
			r.FormValue("slug"), r.FormValue("title"), r.FormValue("body"),
			r.FormValue("status"), now,
			id, db.SiteID, collection,
		)
		if err != nil {
			log.Printf("Error updating record: %v", err)
		}

		http.Redirect(w, r, "/_/collections/"+collection, http.StatusFound)
	}
}

func handleRecordDelete(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		collection := chi.URLParam(r, "collection")
		id := chi.URLParam(r, "id")

		_, err := db.Conn.Exec(
			`DELETE FROM posts WHERE id = ? AND site_id = ? AND collection = ?`,
			id, db.SiteID, collection,
		)
		if err != nil {
			log.Printf("Error deleting record: %v", err)
		}

		http.Redirect(w, r, "/_/collections/"+collection, http.StatusFound)
	}
}

// --- User management handlers ---

func handleUserList(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		users, err := db.ListUsers()
		if err != nil {
			log.Printf("Error listing users: %v", err)
			users = []*data.User{}
		}

		renderPage(w, "users", map[string]any{
			"User":  user,
			"Users": users,
		})
	}
}

func handleUserForm(db *data.DB, editing bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		d := map[string]any{
			"User":    user,
			"Editing": editing,
			"Roles":   availableRoles(user.Role),
		}

		if editing {
			id := chi.URLParam(r, "id")
			target, err := db.GetUserByID(id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			d["Target"] = target
		}

		renderPage(w, "user_form", d)
	}
}

func handleUserCreate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		email := strings.TrimSpace(r.FormValue("email"))
		name := strings.TrimSpace(r.FormValue("name"))
		password := r.FormValue("password")
		role := r.FormValue("role")

		if email == "" {
			renderPage(w, "user_form", map[string]any{"User": user, "Editing": false, "Error": "Email is required.", "Roles": availableRoles(user.Role)})
			return
		}
		if len(password) < 8 {
			renderPage(w, "user_form", map[string]any{"User": user, "Editing": false, "Error": "Password must be at least 8 characters.", "Roles": availableRoles(user.Role), "Email": email, "Name": name})
			return
		}
		if name == "" {
			name = strings.Split(email, "@")[0]
		}

		// Non-superadmins can't create admins.
		if role == "superadmin" || (role == "admin" && user.Role != "superadmin") {
			role = "member"
		}

		_, err := db.CreateUser(email, name, password, role)
		if err != nil {
			renderPage(w, "user_form", map[string]any{"User": user, "Editing": false, "Error": "Failed to create user: " + err.Error(), "Roles": availableRoles(user.Role), "Email": email, "Name": name})
			return
		}

		http.Redirect(w, r, "/_/users", http.StatusFound)
	}
}

func handleUserUpdate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		id := chi.URLParam(r, "id")
		target, err := db.GetUserByID(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Can't edit someone with a higher or equal role (unless superadmin).
		if user.Role != "superadmin" && data.RoleRank(target.Role) >= data.RoleRank(user.Role) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		role := r.FormValue("role")

		if name == "" {
			name = target.Name
		}

		// Can't promote to superadmin or to a role >= your own (unless superadmin).
		if role == "superadmin" || (user.Role != "superadmin" && data.RoleRank(role) >= data.RoleRank(user.Role)) {
			role = target.Role
		}

		if err := db.UpdateUser(id, name, role); err != nil {
			log.Printf("Error updating user: %v", err)
		}

		// Update password if provided.
		newPassword := r.FormValue("password")
		if len(newPassword) >= 8 {
			if err := db.UpdateUserPassword(id, newPassword); err != nil {
				log.Printf("Error updating password: %v", err)
			}
		}

		http.Redirect(w, r, "/_/users", http.StatusFound)
	}
}

func handleUserDelete(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := requireAuth(w, r, db)
		if user == nil {
			return
		}
		if !requireRole(w, user, "admin") {
			return
		}

		id := chi.URLParam(r, "id")

		// Can't delete yourself.
		if id == user.ID {
			http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
			return
		}

		if err := db.DeleteUser(id); err != nil {
			log.Printf("Error deleting user: %v", err)
		}

		http.Redirect(w, r, "/_/users", http.StatusFound)
	}
}

// --- Helpers ---

var contentTypes = []string{
	"blog", "pages", "posts",
}

// availableRoles returns the roles the current user can assign.
func availableRoles(currentRole string) []string {
	switch currentRole {
	case "superadmin":
		return []string{"admin", "editor", "member"}
	case "admin":
		return []string{"editor", "member"}
	default:
		return []string{"member"}
	}
}

func renderPage(w http.ResponseWriter, page string, d map[string]any) {
	t, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "Page not found", 404)
		return
	}

	// The shared nav compares .Active against a string, so make sure the key is
	// always present (a missing map key would make {{eq}} fail at render time).
	if d == nil {
		d = map[string]any{}
	}
	if _, ok := d["Active"]; !ok {
		d["Active"] = ""
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", d); err != nil {
		log.Printf("Admin render error: %v", err)
	}
}

func roleTag(role string) string {
	classes := map[string]string{
		"superadmin": "bg-purple-600",
		"admin":      "bg-blue-600",
		"editor":     "bg-emerald-600",
		"member":     "bg-gray-500",
	}
	c, ok := classes[role]
	if !ok {
		c = "bg-gray-500"
	}
	return `<span class="inline-block rounded-full px-2 py-0.5 text-xs font-semibold text-white ` + c + `">` + role + `</span>`
}
