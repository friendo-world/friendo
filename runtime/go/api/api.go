package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/henryholtgeerts/friendo/runtime/go/data"
)

// Mount registers the sync API routes under /_/api/ on the given router.
// All endpoints require site admin authentication (email + password via session cookie).
func Mount(r chi.Router, db *data.DB, siteDir string, authFunc func(*http.Request) *data.User) {
	r.Route("/_/api", func(r chi.Router) {
		// All API routes require admin auth.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				user := authFunc(req)
				if user == nil {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
					return
				}
				if data.RoleRank(user.Role) < data.RoleRank("admin") {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, req)
			})
		})

		// Push endpoints
		r.Post("/push/templates", handlePushTemplates(siteDir))
		r.Post("/push/assets", handlePushAssets(siteDir))
		r.Post("/push/data", handlePushData(db))
		r.Post("/push/users", handlePushUsers(db))

		// Pull endpoints
		r.Get("/pull/data", handlePullData(db))
		r.Get("/pull/users", handlePullUsers(db))
	})
}

// --- Push handlers ---

// handlePushTemplates accepts template files and writes them to the site directory.
// Expects JSON body: {"files": [{"path": "pages/index.html", "content": "..."}]}
func handlePushTemplates(siteDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Files []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		written := 0
		for _, f := range body.Files {
			// Only allow template directories.
			if !strings.HasPrefix(f.Path, "pages/") && !strings.HasPrefix(f.Path, "templates/") {
				continue
			}
			// Sanitize path to prevent directory traversal.
			cleanPath := filepath.Clean(f.Path)
			if strings.Contains(cleanPath, "..") {
				continue
			}

			fullPath := filepath.Join(siteDir, cleanPath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				log.Printf("Error creating dir for %s: %v", cleanPath, err)
				continue
			}
			if err := os.WriteFile(fullPath, []byte(f.Content), 0o644); err != nil {
				log.Printf("Error writing %s: %v", cleanPath, err)
				continue
			}
			written++
		}

		jsonResponse(w, map[string]int{"written": written})
	}
}

// handlePushAssets accepts static asset files and writes them to public/.
// Expects multipart form or JSON body with base64-encoded content.
// Simple JSON mode: {"files": [{"path": "public/style.css", "content": "..."}]}
func handlePushAssets(siteDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Files []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		written := 0
		for _, f := range body.Files {
			if !strings.HasPrefix(f.Path, "public/") {
				continue
			}
			cleanPath := filepath.Clean(f.Path)
			if strings.Contains(cleanPath, "..") {
				continue
			}

			fullPath := filepath.Join(siteDir, cleanPath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				log.Printf("Error creating dir for %s: %v", cleanPath, err)
				continue
			}
			if err := os.WriteFile(fullPath, []byte(f.Content), 0o644); err != nil {
				log.Printf("Error writing %s: %v", cleanPath, err)
				continue
			}
			written++
		}

		jsonResponse(w, map[string]int{"written": written})
	}
}

// handlePushData upserts records into the local database.
// Expects JSON body: {"records": [{"id": "...", "collection": "blog", ...}]}
func handlePushData(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Records []map[string]any `json:"records"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		synced := 0
		for _, rec := range body.Records {
			str := func(key string) string {
				v, _ := rec[key].(string)
				return v
			}

			id := str("id")
			if id == "" {
				id = data.GenerateID()
			}
			now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

			_, err := db.Conn.Exec(
				`INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, created, updated)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   collection=excluded.collection, slug=excluded.slug, title=excluded.title,
				   body=excluded.body, author_id=excluded.author_id, status=excluded.status,
				   published_at=excluded.published_at, updated=excluded.updated`,
				id, db.SiteID, str("collection"), str("slug"), str("title"),
				str("body"), str("author_id"), str("status"),
				str("published_at"),
				func() string {
					if c := str("created"); c != "" {
						return c
					}
					return now
				}(),
				now,
			)
			if err != nil {
				log.Printf("Error upserting record %s: %v", id, err)
				continue
			}
			synced++
		}

		jsonResponse(w, map[string]int{"synced": synced})
	}
}

// handlePushUsers upserts user accounts into the local database.
// Expects JSON body: {"users": [{"id": "...", "email": "...", ...}]}
func handlePushUsers(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Users []map[string]any `json:"users"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		synced := 0
		for _, u := range body.Users {
			str := func(key string) string {
				v, _ := u[key].(string)
				return v
			}

			id := str("id")
			if id == "" {
				id = data.GenerateID()
			}
			now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

			_, err := db.Conn.Exec(
				`INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   email=excluded.email, phone=excluded.phone, name=excluded.name,
				   avatar=excluded.avatar, password_hash=excluded.password_hash,
				   role=excluded.role, auth_methods=excluded.auth_methods, updated=excluded.updated`,
				id, db.SiteID, str("email"), str("phone"), str("name"),
				str("avatar"), str("password_hash"),
				func() string {
					if r := str("role"); r != "" {
						return r
					}
					return "member"
				}(),
				func() string {
					if a := str("auth_methods"); a != "" {
						return a
					}
					return `["password"]`
				}(),
				func() string {
					if c := str("created"); c != "" {
						return c
					}
					return now
				}(),
				now,
			)
			if err != nil {
				log.Printf("Error upserting user %s: %v", id, err)
				continue
			}
			synced++
		}

		jsonResponse(w, map[string]int{"synced": synced})
	}
}

// --- Pull handlers ---

// handlePullData returns all records as JSON.
func handlePullData(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Conn.Query(
			`SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
			 FROM posts WHERE site_id = ? ORDER BY created DESC`,
			db.SiteID,
		)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var records []map[string]any
		for rows.Next() {
			var id, collection, slug, title, body, authorID, status, publishedAt, created, updated string
			if err := rows.Scan(&id, &collection, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated); err != nil {
				continue
			}
			records = append(records, map[string]any{
				"id":           id,
				"collection":   collection,
				"slug":         slug,
				"title":        title,
				"body":         body,
				"author_id":    authorID,
				"status":       status,
				"published_at": publishedAt,
				"created":      created,
				"updated":      updated,
			})
		}

		if records == nil {
			records = []map[string]any{}
		}
		jsonResponse(w, map[string]any{"records": records})
	}
}

// handlePullUsers returns all user accounts as JSON.
func handlePullUsers(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := db.ListUsers()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}

		var result []map[string]any
		for _, u := range users {
			result = append(result, map[string]any{
				"id":            u.ID,
				"email":         u.Email,
				"phone":         u.Phone,
				"name":          u.Name,
				"avatar":        u.Avatar,
				"password_hash": u.PasswordHash,
				"role":          u.Role,
				"auth_methods":  u.AuthMethods,
				"created":       u.Created,
				"updated":       u.Updated,
			})
		}

		if result == nil {
			result = []map[string]any{}
		}
		jsonResponse(w, map[string]any{"users": result})
	}
}

// --- Helpers ---

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
