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

		// Community: public reads, member-gated writes. These self-gate rather
		// than joining the admin group — reads are open, and a write needs only
		// an authenticated member (any role), not admin.
		r.Get("/posts/{id}/comments", handleListComments(db))
		r.Post("/posts/{id}/comments", handlePostComment(db, authFunc))
		r.Get("/reactions", handleListReactions(db, authFunc))
		r.Post("/reactions", handleToggleReaction(db, authFunc))
		r.Get("/polls/by-slug/{slug}", handleGetPollBySlug(db, authFunc))
		r.Get("/polls/{id}", handleGetPoll(db, authFunc))
		r.Post("/polls/{id}/vote", handleVotePoll(db, authFunc))

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

			// Comment moderation
			r.Get("/comments", handleModerationList(db))
			r.Put("/comments/{id}", handleUpdateComment(db))
			r.Delete("/comments/{id}", handleDeleteComment(db))

			// Polls (creation is admin; reading + voting are public/member)
			r.Post("/polls", handleCreatePoll(db))

			// Users
			r.Get("/users", handleListUsers(db))
			r.Post("/users", handleCreateUser(db, authFunc))
			r.Put("/users/{id}", handleUpdateUser(db, authFunc))
			r.Delete("/users/{id}", handleDeleteUser(db, authFunc))

			// Settings
			r.Get("/settings", handleSettings(db, siteName))
			r.Put("/settings", handleUpdateSettings(db))

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
	Slug   string          `json:"slug"`
	Title  string          `json:"title"`
	Body   string          `json:"body"`
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"` // optional front-matter fields; omitted leaves data untouched
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
		if len(in.Data) > 0 && string(in.Data) != "null" {
			db.SetRecordData(id, string(in.Data))
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
		if len(in.Data) > 0 && string(in.Data) != "null" {
			db.SetRecordData(id, string(in.Data))
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

// --- Comments (community) ---

// validCommentStatus is the set of moderation states a comment may hold.
func validCommentStatus(s string) bool {
	switch s {
	case "pending", "approved", "rejected":
		return true
	}
	return false
}

// handleListComments returns a post's approved comments. Public — anonymous
// visitors see the same approved set. The moderation queue (admin) surfaces
// pending ones separately.
func handleListComments(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		comments, err := db.ListCommentsByPost(chi.URLParam(r, "id"), false)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"comments": comments})
	}
}

// handlePostComment creates a comment on a post. Member-gated: any authenticated
// user may post; the comment starts 'pending' for moderation.
func handlePostComment(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			Body     string `json:"body"`
			ParentID string `json:"parent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Body) == "" {
			jsonError(w, "body is required", http.StatusBadRequest)
			return
		}
		status := "pending"
		if db.GetBoolSetting(settingAutoApprove, false) {
			status = "approved"
		}
		authorID := db.DefaultAuthorID(user.ID)
		id, err := db.CreateComment(chi.URLParam(r, "id"), in.ParentID, authorID, in.Body, status)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		comment, _ := db.GetComment(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"comment": comment})
	}
}

// handleModerationList returns comments filtered by ?status (admin). Defaults to
// the pending queue when no status is given.
func handleModerationList(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		comments, err := db.ListCommentsByStatus(status)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"comments": comments})
	}
}

// handleUpdateComment changes a comment's moderation status (admin).
func handleUpdateComment(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if !validCommentStatus(in.Status) {
			jsonError(w, "invalid status", http.StatusBadRequest)
			return
		}
		err := db.UpdateCommentStatus(chi.URLParam(r, "id"), in.Status)
		if err == sql.ErrNoRows {
			jsonError(w, "comment not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		comment, _ := db.GetComment(chi.URLParam(r, "id"))
		jsonResponse(w, map[string]any{"comment": comment})
	}
}

// handleDeleteComment removes a comment (admin).
func handleDeleteComment(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := db.DeleteComment(chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "comment not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Reactions (community) ---

// handleListReactions returns per-emoji counts for a target (public). If the
// caller is an authenticated member, each entry reports whether they reacted.
func handleListReactions(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType := r.URL.Query().Get("target_type")
		targetID := r.URL.Query().Get("target_id")
		if targetType == "" || targetID == "" {
			jsonError(w, "target_type and target_id are required", http.StatusBadRequest)
			return
		}
		authorID := ""
		if u := authFunc(r); u != nil {
			authorID = db.DefaultAuthorID(u.ID)
		}
		reactions, err := db.ReactionCounts(targetType, targetID, authorID)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"reactions": reactions})
	}
}

// handleToggleReaction adds or removes the caller's reaction (member-gated).
func handleToggleReaction(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Emoji      string `json:"emoji"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if in.TargetType == "" || in.TargetID == "" || in.Emoji == "" {
			jsonError(w, "target_type, target_id and emoji are required", http.StatusBadRequest)
			return
		}
		authorID := db.DefaultAuthorID(user.ID)
		reacted, err := db.ToggleReaction(in.TargetType, in.TargetID, authorID, in.Emoji)
		if err != nil {
			jsonError(w, fmt.Sprintf("reaction error: %v", err), http.StatusInternalServerError)
			return
		}
		reactions, _ := db.ReactionCounts(in.TargetType, in.TargetID, authorID)
		jsonResponse(w, map[string]any{"reacted": reacted, "reactions": reactions})
	}
}

// --- Polls (community) ---

// handleCreatePoll creates a poll (admin-gated).
func handleCreatePoll(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PostID   string   `json:"post_id"`
			Question string   `json:"question"`
			Options  []string `json:"options"`
			ClosesAt string   `json:"closes_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Question) == "" || len(in.Options) < 2 {
			jsonError(w, "question and at least two options are required", http.StatusBadRequest)
			return
		}
		id, err := db.CreatePoll(in.PostID, "", in.Question, in.Options, in.ClosesAt)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		poll, _ := db.GetPoll(id, "")
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"poll": poll})
	}
}

// handleGetPoll returns a poll with tallies (public); includes the caller's vote
// when authenticated.
func handleGetPoll(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authorID := ""
		if u := authFunc(r); u != nil {
			authorID = db.DefaultAuthorID(u.ID)
		}
		poll, err := db.GetPoll(chi.URLParam(r, "id"), authorID)
		if err == data.ErrPollNotFound {
			jsonError(w, "poll not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"poll": poll})
	}
}

// handleGetPollBySlug resolves a poll by its author-chosen slug (public),
// lazily creating it from the declaring post's front matter on first use.
func handleGetPollBySlug(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := db.ResolvePollBySlug(chi.URLParam(r, "slug"))
		if err == data.ErrPollNotFound {
			jsonError(w, "poll not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		authorID := ""
		if u := authFunc(r); u != nil {
			authorID = db.DefaultAuthorID(u.ID)
		}
		poll, err := db.GetPoll(id, authorID)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"poll": poll})
	}
}

// handleVotePoll records a member's vote (member-gated). One vote per member;
// closed polls are rejected.
func handleVotePoll(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			OptionIndex int `json:"option_index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		authorID := db.DefaultAuthorID(user.ID)
		err := db.CastVote(chi.URLParam(r, "id"), authorID, in.OptionIndex)
		switch err {
		case nil:
			// fall through
		case data.ErrPollNotFound:
			jsonError(w, "poll not found", http.StatusNotFound)
			return
		case data.ErrPollClosed:
			jsonError(w, "poll closed", http.StatusForbidden)
			return
		case data.ErrAlreadyVoted:
			jsonError(w, "already voted", http.StatusConflict)
			return
		default:
			jsonError(w, fmt.Sprintf("vote error: %v", err), http.StatusInternalServerError)
			return
		}
		poll, _ := db.GetPoll(chi.URLParam(r, "id"), authorID)
		jsonResponse(w, map[string]any{"poll": poll})
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
			if !strings.HasPrefix(f.Path, "pages/") && !strings.HasPrefix(f.Path, "layouts/") {
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

// handlePushAssets accepts static asset files and writes them to assets/.
// Expects multipart form or JSON body with base64-encoded content.
// Simple JSON mode: {"files": [{"path": "assets/style.css", "content": "..."}]}
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
			if !strings.HasPrefix(f.Path, "assets/") {
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

			// `data` (arbitrary front matter) arrives as a nested object; store as JSON.
			dataJSON := "{}"
			if d, ok := rec["data"]; ok && d != nil {
				if b, err := json.Marshal(d); err == nil {
					dataJSON = string(b)
				}
			}

			_, err := db.Conn.Exec(
				`INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, data, created, updated)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   collection=excluded.collection, slug=excluded.slug, title=excluded.title,
				   body=excluded.body, author_id=excluded.author_id, status=excluded.status,
				   published_at=excluded.published_at, data=excluded.data, updated=excluded.updated`,
				id, db.SiteID, str("collection"), str("slug"), str("title"),
				str("body"), str("author_id"), str("status"),
				str("published_at"), dataJSON,
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
			`SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated, data
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
			var id, collection, slug, title, body, authorID, status, publishedAt, created, updated, dataJSON string
			if err := rows.Scan(&id, &collection, &slug, &title, &body, &authorID, &status, &publishedAt, &created, &updated, &dataJSON); err != nil {
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
				"data":         dataJSON,
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

// otpResendWindow bounds how often an account may request a fresh login code.
const otpResendWindow = 30 * time.Second

// emailConfigured reports whether an email provider is wired up. When it is, the
// code is delivered by email and never echoed in the API response; without it
// (local dev, CI) request-code returns the code so the flow still works.
// Configure by setting RESEND_API_KEY and FRIENDO_EMAIL_FROM in the environment.
func emailConfigured() bool {
	return os.Getenv("RESEND_API_KEY") != "" && os.Getenv("FRIENDO_EMAIL_FROM") != ""
}

// sendOTPEmail delivers a login code via Resend. Best-effort: a delivery error is
// logged, not surfaced, so a provider hiccup never leaks whether an email exists.
func sendOTPEmail(email, code string) {
	if !emailConfigured() {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"from":    os.Getenv("FRIENDO_EMAIL_FROM"),
		"to":      []string{email},
		"subject": "Your sign-in code",
		"text":    fmt.Sprintf("Your code is %s. It expires in 10 minutes.", code),
	})
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", strings.NewReader(string(payload)))
	if err != nil {
		log.Printf("otp email: build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("RESEND_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("otp email: send: %v", err)
		return
	}
	resp.Body.Close()
}

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

		// Rate limit: one live code at a time per account within the window.
		if db.HasFreshOTP(user.ID, otpResendWindow) {
			jsonError(w, "a code was already sent — please wait before requesting another", http.StatusTooManyRequests)
			return
		}

		code := generateOTP()
		if err := db.CreateOTP(user.ID, code, 10*time.Minute); err != nil {
			jsonError(w, "could not issue code", http.StatusInternalServerError)
			return
		}
		sendOTPEmail(email, code)

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

// settingAutoApprove stores whether new comments skip the moderation queue.
const settingAutoApprove = "moderation.auto_approve"

func handleSettings(db *data.DB, siteName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, _ := db.ListUsers()
		counts, _ := db.CollectionCounts()
		jsonResponse(w, map[string]any{
			"site":        map[string]any{"name": siteName},
			"collections": len(counts),
			"users":       len(users),
			"moderation":  map[string]any{"auto_approve": db.GetBoolSetting(settingAutoApprove, false)},
		})
	}
}

// handleUpdateSettings persists admin-configurable settings (currently the
// comment auto-approve toggle).
func handleUpdateSettings(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Moderation *struct {
				AutoApprove *bool `json:"auto_approve"`
			} `json:"moderation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if in.Moderation != nil && in.Moderation.AutoApprove != nil {
			val := "false"
			if *in.Moderation.AutoApprove {
				val = "true"
			}
			if err := db.SetSetting(settingAutoApprove, val); err != nil {
				jsonError(w, fmt.Sprintf("save error: %v", err), http.StatusInternalServerError)
				return
			}
		}
		jsonResponse(w, map[string]any{
			"moderation": map[string]any{"auto_approve": db.GetBoolSetting(settingAutoApprove, false)},
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
