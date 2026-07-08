package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Mount registers the REST + sync API routes under /api on the given router,
// which is expected to be the /_ subrouter (so endpoints live at /_/api/*).
//
// Public endpoints (me, auth) carry no auth so the SPA can bootstrap and log in.
// Everything else requires site admin authentication (session cookie).
func Mount(r chi.Router, db *data.DB, siteDir, siteName string, authFunc func(*http.Request) *data.User) {
	r.Route("/api", func(r chi.Router) {
		// Public endpoints — used by the SPA to bootstrap and authenticate.
		r.Get("/me", handleMe(authFunc))
		r.Post("/auth/login", handleLogin(db))
		r.Post("/auth/logout", handleLogout(db))
		r.Get("/setup", handleSetupStatus(db))
		r.Post("/setup", handleSetupCreate(db))
		r.Post("/migrate", handleMigrate(db))
		r.Post("/auth/request-code", handleRequestCode(db))
		r.Post("/auth/verify-code", handleVerifyCode(db))

		// Authenticated endpoints (admin+).
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					user := authFunc(req)
					if user == nil {
						jsonError(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					if data.RoleRank(user.Role) < data.RoleRank("admin") {
						jsonError(w, "forbidden", http.StatusForbidden)
						return
					}
					next.ServeHTTP(w, req)
				})
			})

			// Content: collections + records
			r.Get("/collections", handleListCollections(db))
			r.Get("/collections/{collection}/records", handleListRecords(db))
			r.Post("/collections/{collection}/records", handleCreateRecord(db, authFunc))
			r.Get("/records/{id}", handleGetRecord(db))
			r.Put("/records/{id}", handleUpdateRecord(db))
			r.Delete("/records/{id}", handleDeleteRecord(db))

			// Users
			r.Get("/users", handleListUsers(db))
			r.Post("/users", handleCreateUser(db, authFunc))
			r.Put("/users/{id}", handleUpdateUser(db, authFunc))
			r.Delete("/users/{id}", handleDeleteUser(db, authFunc))

			// Settings
			r.Get("/settings", handleSettings(db, siteName))

			// Push endpoints
			r.Post("/push/templates", handlePushTemplates(siteDir))
			r.Post("/push/assets", handlePushAssets(siteDir))
			r.Post("/push/data", handlePushData(db))
			r.Post("/push/users", handlePushUsers(db))

			// Pull endpoints
			r.Get("/pull/data", handlePullData(db))
			r.Get("/pull/users", handlePullUsers(db))
		})
	})
}

// --- Content (collections + records) ---

// defaultCollections always appear in the collections list so a fresh site has
// somewhere to create the first record. Both runtimes use the same set.
var defaultCollections = []string{"blog", "pages", "posts"}

func handleListCollections(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counts, err := db.CollectionCounts()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		byName := map[string]int{}
		for _, c := range counts {
			byName[c.Name] = c.Count
		}

		out := []data.CollectionCount{}
		seen := map[string]bool{}
		for _, name := range defaultCollections {
			out = append(out, data.CollectionCount{Name: name, Count: byName[name]})
			seen[name] = true
		}
		for _, c := range counts {
			if !seen[c.Name] {
				out = append(out, c)
			}
		}
		jsonResponse(w, map[string]any{"collections": out})
	}
}

func handleListRecords(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collection := chi.URLParam(r, "collection")
		records, err := db.QueryCollection(collection)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		if records == nil {
			records = []map[string]any{}
		}
		jsonResponse(w, map[string]any{"records": records})
	}
}

func handleGetRecord(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		record, err := db.GetRecordByID(chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "record not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"record": record})
	}
}

type recordInput struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

func (in recordInput) status() string {
	if in.Status == "" {
		return "draft"
	}
	return in.Status
}

func handleCreateRecord(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in recordInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		collection := chi.URLParam(r, "collection")
		authorID := ""
		if u := authFunc(r); u != nil {
			authorID = db.DefaultAuthorID(u.ID)
		}
		id, err := db.CreateRecord(collection, in.Slug, in.Title, in.Body, in.status(), authorID)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		record, _ := db.GetRecordByID(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"record": record})
	}
}

func handleUpdateRecord(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in recordInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		id := chi.URLParam(r, "id")
		err := db.UpdateRecord(id, in.Slug, in.Title, in.Body, in.status())
		if err == sql.ErrNoRows {
			jsonError(w, "record not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		record, _ := db.GetRecordByID(id)
		jsonResponse(w, map[string]any{"record": record})
	}
}

func handleDeleteRecord(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := db.DeleteRecord(chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "record not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Auth (session cookie) ---

// sessionCookieName must match the cookie read by admin.GetSessionUser.
const sessionCookieName = "friendo_session"

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

// userJSON is the public shape of a user returned to the SPA (no password hash).
func userJSON(u *data.User) map[string]any {
	return map[string]any{
		"id":      u.ID,
		"email":   u.Email,
		"name":    u.Name,
		"role":    u.Role,
		"created": u.Created,
	}
}

// handleMe returns the current user, or 401 if not authenticated.
func handleMe(authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

// handleLogin authenticates email + password and starts a session.
func handleLogin(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		user, err := db.AuthenticateUser(strings.TrimSpace(body.Email), body.Password)
		if err != nil {
			jsonError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			jsonError(w, "could not create session", http.StatusInternalServerError)
			return
		}

		setSessionCookie(w, token)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

// handleLogout ends the current session.
func handleLogout(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			db.DeleteSession(cookie.Value)
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
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

// --- First-run setup / migrate ---

func handleSetupStatus(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]any{
			"needsSetup":     !db.IsSetupDone(),
			"hasLegacyAdmin": db.HasLegacyAdmin(),
		})
	}
}

func handleSetupCreate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db.IsSetupDone() {
			jsonError(w, "setup already complete", http.StatusConflict)
			return
		}
		var in struct {
			Email    string `json:"email"`
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		if len(in.Password) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}

		user, err := db.CreateUser(email, name, in.Password, "superadmin")
		if err != nil {
			jsonError(w, "could not create account: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent()); err == nil {
			setSessionCookie(w, token)
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

func handleMigrate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.HasLegacyAdmin() {
			jsonError(w, "no legacy admin to migrate", http.StatusBadRequest)
			return
		}
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		if err := db.MigrateAdminToUsers(email); err != nil {
			jsonError(w, "migration failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		jsonResponse(w, map[string]any{"ok": true})
	}
}

// --- OTP (passwordless member login) ---

// emailConfigured reports whether an email provider is wired up. Until one is
// (Phase 3e), request-code returns the code in its response for dev/test.
func emailConfigured() bool { return false }

func generateOTP() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// handleRequestCode finds-or-creates a member account for the email and issues a
// one-time login code.
func handleRequestCode(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}

		user, err := db.GetUserByEmail(email)
		if err != nil {
			user, err = db.CreateMember(email, "")
			if err != nil {
				jsonError(w, "could not create account: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		code := generateOTP()
		if err := db.CreateOTP(user.ID, code, 10*time.Minute); err != nil {
			jsonError(w, "could not issue code", http.StatusInternalServerError)
			return
		}
		// TODO (Phase 3e): send the code by email.

		resp := map[string]any{"sent": true}
		if !emailConfigured() {
			resp["code"] = code // dev/test only — no email provider configured
		}
		jsonResponse(w, resp)
	}
}

// handleVerifyCode validates a one-time code and starts a session.
func handleVerifyCode(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		user, err := db.GetUserByEmail(strings.TrimSpace(in.Email))
		if err != nil || !db.VerifyOTP(user.ID, in.Code) {
			jsonError(w, "invalid or expired code", http.StatusUnauthorized)
			return
		}
		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			jsonError(w, "could not create session", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, token)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

// --- Users ---

// canAssignRole reports whether an actor may assign targetRole. superadmin is
// never assignable via the API; only a superadmin may assign admin.
func canAssignRole(actorRole, targetRole string) bool {
	switch targetRole {
	case "admin", "editor", "member":
	default:
		return false
	}
	if targetRole == "admin" && actorRole != "superadmin" {
		return false
	}
	return true
}

func handleListUsers(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := db.ListUsers()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		out := []map[string]any{}
		for _, u := range users {
			out = append(out, userJSON(u))
		}
		jsonResponse(w, map[string]any{"users": out})
	}
}

func handleCreateUser(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := authFunc(r)
		var in struct {
			Email    string `json:"email"`
			Name     string `json:"name"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		if len(in.Password) < 8 {
			jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		role := in.Role
		if role == "" {
			role = "member"
		}
		if !canAssignRole(actor.Role, role) {
			jsonError(w, "you cannot assign that role", http.StatusForbidden)
			return
		}
		user, err := db.CreateUser(email, name, in.Password, role)
		if err != nil {
			jsonError(w, "could not create user: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

func handleUpdateUser(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := authFunc(r)
		id := chi.URLParam(r, "id")
		target, err := db.GetUserByID(id)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		// Can't modify someone of equal/higher rank unless superadmin.
		if actor.Role != "superadmin" && data.RoleRank(target.Role) >= data.RoleRank(actor.Role) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}

		var in struct {
			Name     string `json:"name"`
			Role     string `json:"role"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = target.Name
		}
		role := target.Role
		if in.Role != "" && in.Role != target.Role {
			if !canAssignRole(actor.Role, in.Role) {
				jsonError(w, "you cannot assign that role", http.StatusForbidden)
				return
			}
			role = in.Role
		}
		if err := db.UpdateUser(id, name, role); err != nil {
			jsonError(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if len(in.Password) >= 8 {
			if err := db.UpdateUserPassword(id, in.Password); err != nil {
				jsonError(w, "password update failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		updated, _ := db.GetUserByID(id)
		jsonResponse(w, map[string]any{"user": userJSON(updated)})
	}
}

func handleDeleteUser(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := authFunc(r)
		id := chi.URLParam(r, "id")
		if id == actor.ID {
			jsonError(w, "you cannot delete your own account", http.StatusBadRequest)
			return
		}
		target, err := db.GetUserByID(id)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		if actor.Role != "superadmin" && data.RoleRank(target.Role) >= data.RoleRank(actor.Role) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := db.DeleteUser(id); err != nil {
			jsonError(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Settings ---

func handleSettings(db *data.DB, siteName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, _ := db.ListUsers()
		counts, _ := db.CollectionCounts()
		jsonResponse(w, map[string]any{
			"site":        map[string]any{"name": siteName},
			"collections": len(counts),
			"users":       len(users),
		})
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
