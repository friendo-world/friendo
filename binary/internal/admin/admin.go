package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/henryholtgeerts/friendo/binary/internal/data"
)

// openAdminMode is set when --open-admin is passed to friendo serve.
// When true, the admin UI skips password authentication.
var openAdminMode bool

// Mount registers the admin UI routes under /_/ on the given router.
// If openAdmin is true, the admin UI is accessible without a password.
func Mount(r chi.Router, db *data.DB, openAdmin bool) {
	openAdminMode = openAdmin
	r.Route("/_", func(r chi.Router) {
		r.Get("/", handleDashboard(db))
		r.Get("/setup", handleSetupForm(db))
		r.Post("/setup", handleSetupSubmit(db))
		r.Get("/login", handleLoginForm(db))
		r.Post("/login", handleLoginSubmit(db))
		r.Post("/logout", handleLogout())
		r.Get("/collections/{collection}", handleCollectionList(db))
		r.Get("/collections/{collection}/new", handleRecordForm(db, false))
		r.Post("/collections/{collection}/new", handleRecordCreate(db))
		r.Get("/collections/{collection}/{id}/edit", handleRecordForm(db, true))
		r.Post("/collections/{collection}/{id}/edit", handleRecordUpdate(db))
		r.Post("/collections/{collection}/{id}/delete", handleRecordDelete(db))
	})
}

// --- Auth helpers ---

func isSetupDone(db *data.DB) bool {
	var hash string
	err := db.Conn.QueryRow(`SELECT password_hash FROM admin WHERE id = 'admin'`).Scan(&hash)
	return err == nil && hash != ""
}

func checkSession(r *http.Request) bool {
	if openAdminMode {
		return true
	}
	cookie, err := r.Cookie("friendo_admin")
	return err == nil && cookie.Value != ""
}

func setSession(w http.ResponseWriter) {
	token := make([]byte, 32)
	rand.Read(token)
	http.SetCookie(w, &http.Cookie{
		Name:     "friendo_admin",
		Value:    hex.EncodeToString(token),
		Path:     "/_/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 7,
	})
}

// --- Handlers ---

func handleDashboard(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !openAdminMode && !isSetupDone(db) {
			http.Redirect(w, r, "/_/setup", http.StatusFound)
			return
		}
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
			return
		}

		collections, _ := db.ListCollections()
		renderPage(w, "dashboard", map[string]any{
			"Collections":  collections,
			"ContentTypes": contentTypes,
		})
	}
}

func handleSetupForm(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isSetupDone(db) {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}
		renderPage(w, "setup", nil)
	}
}

func handleSetupSubmit(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isSetupDone(db) {
			http.Redirect(w, r, "/_/", http.StatusFound)
			return
		}

		password := r.FormValue("password")
		if len(password) < 6 {
			renderPage(w, "setup", map[string]any{"Error": "Password must be at least 6 characters."})
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			renderPage(w, "setup", map[string]any{"Error": "Internal error."})
			return
		}

		_, err = db.Conn.Exec(
			`INSERT INTO admin (id, password_hash) VALUES ('admin', ?)
			 ON CONFLICT(id) DO UPDATE SET password_hash = ?`,
			string(hash), string(hash),
		)
		if err != nil {
			renderPage(w, "setup", map[string]any{"Error": "Database error."})
			return
		}

		setSession(w)
		http.Redirect(w, r, "/_/", http.StatusFound)
	}
}

func handleLoginForm(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isSetupDone(db) {
			http.Redirect(w, r, "/_/setup", http.StatusFound)
			return
		}
		renderPage(w, "login", nil)
	}
}

func handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "friendo_admin",
			Value:    "",
			Path:     "/_/",
			HttpOnly: true,
			MaxAge:   -1,
		})
		http.Redirect(w, r, "/_/login", http.StatusFound)
	}
}

func handleLoginSubmit(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		password := r.FormValue("password")

		var hash string
		err := db.Conn.QueryRow(`SELECT password_hash FROM admin WHERE id = 'admin'`).Scan(&hash)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
			renderPage(w, "login", map[string]any{"Error": "Invalid password."})
			return
		}

		setSession(w)
		http.Redirect(w, r, "/_/", http.StatusFound)
	}
}

func handleCollectionList(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
			return
		}

		collection := chi.URLParam(r, "collection")
		records, err := db.QueryCollection(collection)
		if err != nil {
			log.Printf("Error querying collection %q: %v", collection, err)
			records = []map[string]any{}
		}

		renderPage(w, "collection", map[string]any{
			"Collection": collection,
			"Records":    records,
		})
	}
}

func handleRecordForm(db *data.DB, editing bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
			return
		}

		collection := chi.URLParam(r, "collection")
		d := map[string]any{
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
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
			return
		}

		collection := chi.URLParam(r, "collection")
		id := generateID()
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

		_, err := db.Conn.Exec(
			`INSERT INTO posts (id, site_id, collection, slug, title, body, status, created, updated)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, db.SiteID, collection,
			r.FormValue("slug"), r.FormValue("title"), r.FormValue("body"),
			r.FormValue("status"), now, now,
		)
		if err != nil {
			log.Printf("Error creating record: %v", err)
			renderPage(w, "record_form", map[string]any{
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
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
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
		if !checkSession(r) {
			http.Redirect(w, r, "/_/login", http.StatusFound)
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

// --- Helpers ---

var contentTypes = []string{
	"blog", "pages", "posts",
}

func generateID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func renderPage(w http.ResponseWriter, page string, data map[string]any) {
	t, err := template.New(page).Funcs(template.FuncMap{
		"json": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
		"join": strings.Join,
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

	t, err = t.New("content").Parse(pageHTML)
	if err != nil {
		http.Error(w, "Template error", 500)
		log.Printf("Admin page template parse error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, page, data); err != nil {
		log.Printf("Admin render error: %v", err)
	}
}

// --- Embedded HTML templates ---

const baseLayout = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Friendo Admin</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; color: #333; line-height: 1.6; }
        .container { max-width: 800px; margin: 0 auto; padding: 2rem; }
        h1 { margin-bottom: 1.5rem; font-size: 1.5rem; }
        h2 { margin-bottom: 1rem; font-size: 1.25rem; }
        a { color: #2563eb; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .card { background: white; border-radius: 8px; padding: 1.5rem; margin-bottom: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
        .btn { display: inline-block; padding: 0.5rem 1rem; border-radius: 6px; border: none; cursor: pointer; font-size: 0.9rem; text-decoration: none; }
        .btn-primary { background: #2563eb; color: white; }
        .btn-primary:hover { background: #1d4ed8; text-decoration: none; }
        .btn-danger { background: #dc2626; color: white; }
        .btn-danger:hover { background: #b91c1c; }
        .btn-sm { padding: 0.25rem 0.5rem; font-size: 0.8rem; }
        input[type="text"], input[type="password"], textarea, select {
            width: 100%; padding: 0.5rem; border: 1px solid #d1d5db; border-radius: 6px;
            font-size: 0.9rem; font-family: inherit; margin-top: 0.25rem;
        }
        textarea { min-height: 150px; resize: vertical; }
        label { display: block; margin-bottom: 1rem; font-weight: 500; }
        .error { background: #fef2f2; color: #dc2626; padding: 0.75rem; border-radius: 6px; margin-bottom: 1rem; }
        .nav { background: white; border-bottom: 1px solid #e5e7eb; padding: 0.75rem 2rem; margin-bottom: 2rem; display: flex; align-items: center; gap: 1.5rem; }
        .nav-brand { font-weight: 700; color: #333; }
        table { width: 100%; border-collapse: collapse; }
        th, td { text-align: left; padding: 0.5rem; border-bottom: 1px solid #e5e7eb; }
        th { font-weight: 600; font-size: 0.85rem; color: #6b7280; }
        .actions { display: flex; gap: 0.5rem; }
    </style>
</head>
<body>
    {{template "content" .}}
</body>
</html>`

var pages = map[string]string{
	"setup": `{{define "setup"}}
<div class="container" style="max-width: 400px; margin-top: 4rem;">
    <div class="card">
        <h1>Set up Friendo</h1>
        <p style="margin-bottom: 1.5rem; color: #6b7280;">Create a password for the admin UI.</p>
        {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
        <form method="POST" action="/_/setup">
            <label>Password
                <input type="password" name="password" required autofocus>
            </label>
            <button type="submit" class="btn btn-primary" style="width:100%">Create password</button>
        </form>
    </div>
</div>
{{end}}`,

	"login": `{{define "login"}}
<div class="container" style="max-width: 400px; margin-top: 4rem;">
    <div class="card">
        <h1>Friendo Admin</h1>
        {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
        <form method="POST" action="/_/login">
            <label>Password
                <input type="password" name="password" required autofocus>
            </label>
            <button type="submit" class="btn btn-primary" style="width:100%">Log in</button>
        </form>
    </div>
</div>
{{end}}`,

	"dashboard": `{{define "dashboard"}}
<div class="nav">
    <span class="nav-brand">Friendo Admin</span>
    <a href="/">View site</a>
    <form method="POST" action="/_/logout" style="margin-left:auto"><button type="submit" class="btn btn-sm">Log out</button></form>
</div>
<div class="container">
    <h1>Collections</h1>
    {{range .ContentTypes}}
    <div class="card" style="display: flex; justify-content: space-between; align-items: center;">
        <div>
            <strong>{{.}}</strong>
        </div>
        <div class="actions">
            <a href="/_/collections/{{.}}" class="btn btn-primary btn-sm">Browse</a>
            <a href="/_/collections/{{.}}/new" class="btn btn-primary btn-sm">New</a>
        </div>
    </div>
    {{end}}
</div>
{{end}}`,

	"collection": `{{define "collection"}}
<div class="nav">
    <span class="nav-brand">Friendo Admin</span>
    <a href="/_/">Dashboard</a>
    <a href="/">View site</a>
    <form method="POST" action="/_/logout" style="margin-left:auto"><button type="submit" class="btn btn-sm">Log out</button></form>
</div>
<div class="container">
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 1.5rem;">
        <h1 style="margin-bottom: 0;">{{.Collection}}</h1>
        <a href="/_/collections/{{.Collection}}/new" class="btn btn-primary">New record</a>
    </div>
    {{if .Records}}
    <div class="card">
        <table>
            <thead>
                <tr>
                    <th>Title</th>
                    <th>Slug</th>
                    <th>Status</th>
                    <th>Created</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>
                {{range .Records}}
                <tr>
                    <td>{{index . "title"}}</td>
                    <td><code>{{index . "slug"}}</code></td>
                    <td>{{index . "status"}}</td>
                    <td>{{index . "created"}}</td>
                    <td class="actions">
                        <a href="/_/collections/{{$.Collection}}/{{index . "id"}}/edit" class="btn btn-primary btn-sm">Edit</a>
                        <form method="POST" action="/_/collections/{{$.Collection}}/{{index . "id"}}/delete" style="display:inline" onsubmit="return confirm('Delete this record?')">
                            <button type="submit" class="btn btn-danger btn-sm">Delete</button>
                        </form>
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{else}}
    <div class="card">
        <p style="color: #6b7280;">No records yet. <a href="/_/collections/{{.Collection}}/new">Create one</a>.</p>
    </div>
    {{end}}
</div>
{{end}}`,

	"record_form": `{{define "record_form"}}
<div class="nav">
    <span class="nav-brand">Friendo Admin</span>
    <a href="/_/">Dashboard</a>
    <a href="/_/collections/{{.Collection}}">{{.Collection}}</a>
    <a href="/">View site</a>
    <form method="POST" action="/_/logout" style="margin-left:auto"><button type="submit" class="btn btn-sm">Log out</button></form>
</div>
<div class="container">
    <h1>{{if .Editing}}Edit record{{else}}New {{.Collection}} record{{end}}</h1>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    <div class="card">
        <form method="POST">
            <label>Title
                <input type="text" name="title" value="{{if .Editing}}{{index .Record "title"}}{{end}}" required>
            </label>
            <label>Slug
                <input type="text" name="slug" value="{{if .Editing}}{{index .Record "slug"}}{{end}}" required>
            </label>
            <label>Body
                <textarea name="body">{{if .Editing}}{{index .Record "body"}}{{end}}</textarea>
            </label>
            <label>Status
                <select name="status">
                    <option value="draft" {{if .Editing}}{{if eq (index .Record "status") "draft"}}selected{{end}}{{end}}>Draft</option>
                    <option value="published" {{if .Editing}}{{if eq (index .Record "status") "published"}}selected{{end}}{{end}}>Published</option>
                </select>
            </label>
            <button type="submit" class="btn btn-primary">{{if .Editing}}Save{{else}}Create{{end}}</button>
        </form>
    </div>
</div>
{{end}}`,
}
