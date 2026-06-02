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

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips password authentication.
var openAdminMode bool

// Mount registers the admin UI routes under /_/ on the given router.
// If openAdmin is true, the admin UI is accessible without a password.
func Mount(r chi.Router, db *data.DB, openAdmin bool) {
	openAdminMode = openAdmin
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

		collections, _ := db.ListCollections()
		renderPage(w, "dashboard", map[string]any{
			"User":         user,
			"Collections":  collections,
			"ContentTypes": contentTypes,
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
	t, err := template.New("base").Funcs(template.FuncMap{
		"json": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
		"join":    strings.Join,
		"roleTag": func(role string) template.HTML { return template.HTML(roleTag(role)) },
	}).Parse(baseLayout)
	if err != nil {
		http.Error(w, "Template error", 500)
		log.Printf("Admin template parse error: %v", err)
		return
	}

	pageHTML, ok := pages[page]
	if !ok {
		http.Error(w, "Page not found", 404)
		return
	}

	// Parse the page template. It defines itself as {{define "pagename"}}...{{end}}.
	// We also add a "content" template that calls the page by name.
	t, err = t.Parse(pageHTML)
	if err != nil {
		http.Error(w, "Template error", 500)
		log.Printf("Admin page template parse error: %v", err)
		return
	}
	t, err = t.New("content").Parse(`{{template "` + page + `" .}}`)
	if err != nil {
		http.Error(w, "Template error", 500)
		log.Printf("Admin content bridge parse error: %v", err)
		return
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

// --- Embedded HTML templates ---

const baseLayout = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Friendo Admin</title>
    <link rel="stylesheet" href="/_/static/admin.css">
</head>
<body class="bg-gray-50 text-gray-900 antialiased">
    {{template "content" .}}
</body>
</html>`

var pages = map[string]string{
	"setup": `{{define "setup"}}
<div class="mx-auto mt-16 max-w-md px-4">
    <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-1 text-xl font-bold">Set up Friendo</h1>
        <p class="mb-6 text-sm text-gray-500">Create your admin account to get started.</p>
        {{if .Error}}<div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{{.Error}}</div>{{end}}
        <form method="POST" action="/_/setup">
            <label class="mb-4 block text-sm font-medium">Email
                <input type="email" name="email" value="{{if .Email}}{{.Email}}{{end}}" required autofocus class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-4 block text-sm font-medium">Name <span class="font-normal text-gray-400">(optional)</span>
                <input type="text" name="name" value="{{if .Name}}{{.Name}}{{end}}" placeholder="Defaults to email prefix" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-1 block text-sm font-medium">Password
                <input type="password" name="password" required minlength="8" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <p class="mb-5 text-xs text-gray-400">Minimum 8 characters</p>
            <button type="submit" class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">Create account</button>
        </form>
    </div>
</div>
{{end}}`,

	"migrate": `{{define "migrate"}}
<div class="mx-auto mt-16 max-w-md px-4">
    <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-1 text-xl font-bold">Upgrade your admin</h1>
        <p class="mb-6 text-sm text-gray-500">Friendo now uses email-based accounts. Enter your email to upgrade your existing admin password to a full account.</p>
        {{if .Error}}<div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{{.Error}}</div>{{end}}
        <form method="POST" action="/_/migrate">
            <label class="mb-4 block text-sm font-medium">Email
                <input type="email" name="email" required autofocus class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <button type="submit" class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">Upgrade account</button>
        </form>
    </div>
</div>
{{end}}`,

	"login": `{{define "login"}}
<div class="mx-auto mt-16 max-w-md px-4">
    <div class="rounded-lg bg-white p-6 shadow-sm">
        <h1 class="mb-6 text-xl font-bold">Friendo Admin</h1>
        {{if .Error}}<div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{{.Error}}</div>{{end}}
        <form method="POST" action="/_/login">
            <label class="mb-4 block text-sm font-medium">Email
                <input type="email" name="email" value="{{if .Email}}{{.Email}}{{end}}" required autofocus class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-5 block text-sm font-medium">Password
                <input type="password" name="password" required class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <button type="submit" class="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">Log in</button>
        </form>
    </div>
</div>
{{end}}`,

	"dashboard": `{{define "dashboard"}}
<nav class="flex items-center gap-6 border-b border-gray-200 bg-white px-6 py-3">
    <span class="font-bold text-gray-900">Friendo</span>
    <a href="/_/" class="text-sm font-medium text-blue-600">Dashboard</a>
    <a href="/_/users" class="text-sm text-gray-500 hover:text-gray-900">Users</a>
    <a href="/" class="text-sm text-gray-500 hover:text-gray-900">View site</a>
    <div class="ml-auto flex items-center gap-3">
        <span class="text-xs text-gray-400">{{.User.Email}}</span>
        <form method="POST" action="/_/logout"><button type="submit" class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">Log out</button></form>
    </div>
</nav>
<div class="mx-auto max-w-3xl px-4 py-8">
    <h1 class="mb-6 text-xl font-bold">Collections</h1>
    {{range .ContentTypes}}
    <div class="mb-3 flex items-center justify-between rounded-lg bg-white p-4 shadow-sm">
        <span class="font-medium">{{.}}</span>
        <div class="flex gap-2">
            <a href="/_/collections/{{.}}" class="rounded bg-blue-600 px-3 py-1 text-xs font-medium text-white hover:bg-blue-700">Browse</a>
            <a href="/_/collections/{{.}}/new" class="rounded border border-blue-600 px-3 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50">New</a>
        </div>
    </div>
    {{end}}
</div>
{{end}}`,

	"collection": `{{define "collection"}}
<nav class="flex items-center gap-6 border-b border-gray-200 bg-white px-6 py-3">
    <span class="font-bold text-gray-900">Friendo</span>
    <a href="/_/" class="text-sm text-gray-500 hover:text-gray-900">Dashboard</a>
    <a href="/_/users" class="text-sm text-gray-500 hover:text-gray-900">Users</a>
    <a href="/" class="text-sm text-gray-500 hover:text-gray-900">View site</a>
    <div class="ml-auto flex items-center gap-3">
        <span class="text-xs text-gray-400">{{.User.Email}}</span>
        <form method="POST" action="/_/logout"><button type="submit" class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">Log out</button></form>
    </div>
</nav>
<div class="mx-auto max-w-3xl px-4 py-8">
    <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">{{.Collection}}</h1>
        <a href="/_/collections/{{.Collection}}/new" class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">New record</a>
    </div>
    {{if .Records}}
    <div class="overflow-hidden rounded-lg bg-white shadow-sm">
        <table class="w-full">
            <thead>
                <tr class="border-b border-gray-200">
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Title</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Slug</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Status</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Created</th>
                    <th class="px-4 py-3"></th>
                </tr>
            </thead>
            <tbody>
                {{range .Records}}
                <tr class="border-b border-gray-100 hover:bg-gray-50">
                    <td class="px-4 py-3 text-sm font-medium">{{index . "title"}}</td>
                    <td class="px-4 py-3 text-sm"><code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs">{{index . "slug"}}</code></td>
                    <td class="px-4 py-3 text-sm">{{index . "status"}}</td>
                    <td class="px-4 py-3 text-xs text-gray-400">{{index . "created"}}</td>
                    <td class="px-4 py-3">
                        <div class="flex gap-2 justify-end">
                            <a href="/_/collections/{{$.Collection}}/{{index . "id"}}/edit" class="rounded bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">Edit</a>
                            <form method="POST" action="/_/collections/{{$.Collection}}/{{index . "id"}}/delete" class="inline" onsubmit="return confirm('Delete this record?')">
                                <button type="submit" class="rounded bg-red-600 px-2 py-1 text-xs font-medium text-white hover:bg-red-700">Delete</button>
                            </form>
                        </div>
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{else}}
    <div class="rounded-lg bg-white p-6 text-center shadow-sm">
        <p class="text-sm text-gray-500">No records yet. <a href="/_/collections/{{.Collection}}/new" class="text-blue-600 hover:underline">Create one</a>.</p>
    </div>
    {{end}}
</div>
{{end}}`,

	"record_form": `{{define "record_form"}}
<nav class="flex items-center gap-6 border-b border-gray-200 bg-white px-6 py-3">
    <span class="font-bold text-gray-900">Friendo</span>
    <a href="/_/" class="text-sm text-gray-500 hover:text-gray-900">Dashboard</a>
    <a href="/_/collections/{{.Collection}}" class="text-sm text-gray-500 hover:text-gray-900">{{.Collection}}</a>
    <a href="/" class="text-sm text-gray-500 hover:text-gray-900">View site</a>
    <div class="ml-auto flex items-center gap-3">
        <span class="text-xs text-gray-400">{{.User.Email}}</span>
        <form method="POST" action="/_/logout"><button type="submit" class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">Log out</button></form>
    </div>
</nav>
<div class="mx-auto max-w-3xl px-4 py-8">
    <h1 class="mb-6 text-xl font-bold">{{if .Editing}}Edit record{{else}}New {{.Collection}} record{{end}}</h1>
    {{if .Error}}<div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{{.Error}}</div>{{end}}
    <div class="rounded-lg bg-white p-6 shadow-sm">
        <form method="POST">
            <label class="mb-4 block text-sm font-medium">Title
                <input type="text" name="title" value="{{if .Editing}}{{index .Record "title"}}{{end}}" required class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-4 block text-sm font-medium">Slug
                <input type="text" name="slug" value="{{if .Editing}}{{index .Record "slug"}}{{end}}" required class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-4 block text-sm font-medium">Body
                <textarea name="body" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm min-h-40 resize-y focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">{{if .Editing}}{{index .Record "body"}}{{end}}</textarea>
            </label>
            <label class="mb-5 block text-sm font-medium">Status
                <select name="status" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
                    <option value="draft" {{if .Editing}}{{if eq (index .Record "status") "draft"}}selected{{end}}{{end}}>Draft</option>
                    <option value="published" {{if .Editing}}{{if eq (index .Record "status") "published"}}selected{{end}}{{end}}>Published</option>
                </select>
            </label>
            <button type="submit" class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">{{if .Editing}}Save{{else}}Create{{end}}</button>
        </form>
    </div>
</div>
{{end}}`,

	"users": `{{define "users"}}
<nav class="flex items-center gap-6 border-b border-gray-200 bg-white px-6 py-3">
    <span class="font-bold text-gray-900">Friendo</span>
    <a href="/_/" class="text-sm text-gray-500 hover:text-gray-900">Dashboard</a>
    <a href="/_/users" class="text-sm font-medium text-blue-600">Users</a>
    <a href="/" class="text-sm text-gray-500 hover:text-gray-900">View site</a>
    <div class="ml-auto flex items-center gap-3">
        <span class="text-xs text-gray-400">{{.User.Email}}</span>
        <form method="POST" action="/_/logout"><button type="submit" class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">Log out</button></form>
    </div>
</nav>
<div class="mx-auto max-w-3xl px-4 py-8">
    <div class="mb-6 flex items-center justify-between">
        <h1 class="text-xl font-bold">Users</h1>
        <a href="/_/users/new" class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">Add user</a>
    </div>
    {{if .Users}}
    <div class="overflow-hidden rounded-lg bg-white shadow-sm">
        <table class="w-full">
            <thead>
                <tr class="border-b border-gray-200">
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Name</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Email</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Role</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase">Created</th>
                    <th class="px-4 py-3"></th>
                </tr>
            </thead>
            <tbody>
                {{range .Users}}
                <tr class="border-b border-gray-100 hover:bg-gray-50">
                    <td class="px-4 py-3 text-sm font-medium">{{.Name}}</td>
                    <td class="px-4 py-3 text-sm text-gray-600">{{.Email}}</td>
                    <td class="px-4 py-3">{{roleTag .Role}}</td>
                    <td class="px-4 py-3 text-xs text-gray-400">{{.Created}}</td>
                    <td class="px-4 py-3">
                        <div class="flex gap-2 justify-end">
                            <a href="/_/users/{{.ID}}/edit" class="rounded bg-blue-600 px-2 py-1 text-xs font-medium text-white hover:bg-blue-700">Edit</a>
                            {{if ne .Role "superadmin"}}
                            <form method="POST" action="/_/users/{{.ID}}/delete" class="inline" onsubmit="return confirm('Delete this user?')">
                                <button type="submit" class="rounded bg-red-600 px-2 py-1 text-xs font-medium text-white hover:bg-red-700">Delete</button>
                            </form>
                            {{end}}
                        </div>
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{else}}
    <div class="rounded-lg bg-white p-6 text-center shadow-sm">
        <p class="text-sm text-gray-500">No users yet.</p>
    </div>
    {{end}}
</div>
{{end}}`,

	"user_form": `{{define "user_form"}}
<nav class="flex items-center gap-6 border-b border-gray-200 bg-white px-6 py-3">
    <span class="font-bold text-gray-900">Friendo</span>
    <a href="/_/" class="text-sm text-gray-500 hover:text-gray-900">Dashboard</a>
    <a href="/_/users" class="text-sm text-gray-500 hover:text-gray-900">Users</a>
    <a href="/" class="text-sm text-gray-500 hover:text-gray-900">View site</a>
    <div class="ml-auto flex items-center gap-3">
        <span class="text-xs text-gray-400">{{.User.Email}}</span>
        <form method="POST" action="/_/logout"><button type="submit" class="rounded border border-gray-300 px-2 py-1 text-xs text-gray-600 hover:bg-gray-50">Log out</button></form>
    </div>
</nav>
<div class="mx-auto max-w-3xl px-4 py-8">
    <h1 class="mb-6 text-xl font-bold">{{if .Editing}}Edit user{{else}}Add user{{end}}</h1>
    {{if .Error}}<div class="mb-4 rounded-md bg-red-50 px-3 py-2 text-sm text-red-600">{{.Error}}</div>{{end}}
    <div class="rounded-lg bg-white p-6 shadow-sm">
        <form method="POST">
            {{if not .Editing}}
            <label class="mb-4 block text-sm font-medium">Email
                <input type="email" name="email" value="{{if .Email}}{{.Email}}{{end}}" required class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            {{else}}
            <div class="mb-4 text-sm">
                <span class="font-medium text-gray-500">Email:</span> <span>{{.Target.Email}}</span>
            </div>
            {{end}}
            <label class="mb-4 block text-sm font-medium">Name
                <input type="text" name="name" value="{{if .Editing}}{{.Target.Name}}{{else}}{{if .Name}}{{.Name}}{{end}}{{end}}" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <label class="mb-4 block text-sm font-medium">Role
                <select name="role" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
                    {{range .Roles}}
                    <option value="{{.}}" {{if $.Editing}}{{if eq $.Target.Role .}}selected{{end}}{{end}}>{{.}}</option>
                    {{end}}
                </select>
            </label>
            <label class="mb-1 block text-sm font-medium">Password{{if .Editing}} <span class="font-normal text-gray-400">(leave blank to keep current)</span>{{end}}
                <input type="password" name="password" {{if not .Editing}}required minlength="8"{{end}} class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none">
            </label>
            <p class="mb-5 text-xs text-gray-400">Minimum 8 characters</p>
            <button type="submit" class="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700">{{if .Editing}}Save{{else}}Create user{{end}}</button>
        </form>
    </div>
</div>
{{end}}`,
}
