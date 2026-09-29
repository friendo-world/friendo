package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Follows API (v0.6 Tier B). Every route is behind the `follows` switch.
//
//	GET    /_/api/follows?author_id=… | slug=…   counts + whether the viewer follows them
//	POST   /_/api/follows {profile_id | slug}     toggle (follow / unfollow), member
//	DELETE /_/api/follows/{profile_id}            unfollow, member
//	GET    /_/api/profiles/{slug}/followers      who follows them (profile visibility applies)
//	GET    /_/api/profiles/{slug}/following      who they follow

// requireAuthor answers the two things every social write needs: a signed-in
// account (401) that has a profile to act as (403 — the localhost open-admin
// owner has none, and a write keyed on an empty author id would be a ghost row).
func requireAuthor(db *data.DB, w http.ResponseWriter, user *data.User) (string, bool) {
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	authorID := db.DefaultAuthorID(user.ID)
	if authorID == "" {
		jsonError(w, "sign in with an account to do that", http.StatusForbidden)
		return "", false
	}
	return authorID, true
}

// resolveAuthor finds a profile by id or address; "" when neither is given or found.
func resolveAuthor(db *data.DB, authorID, slug string) string {
	if authorID != "" {
		if _, err := db.ProfileByID(authorID); err == nil {
			return authorID
		}
		return ""
	}
	if slug != "" {
		if p, err := db.ProfileBySlug(slug); err == nil {
			return p["id"].(string)
		}
	}
	return ""
}

// followPayload is what every follows call answers with: the target, whether the
// viewer follows them, and both counts.
func followPayload(db *data.DB, viewerAuthorID, targetID string) map[string]any {
	followers, following := db.FollowCounts(targetID)
	return map[string]any{
		"profile_id":      targetID,
		"following":       viewerAuthorID != "" && db.IsFollowing(viewerAuthorID, targetID),
		"followers":       followers,
		"following_count": following,
	}
}

// handleFollowStatus: counts for a profile, and whether the viewer follows them.
func handleFollowStatus(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := resolveAuthor(db, r.URL.Query().Get("profile_id"), r.URL.Query().Get("slug"))
		if target == "" {
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		}
		viewer := ""
		if user := authFunc(r); user != nil {
			viewer = db.DefaultAuthorID(user.ID)
		}
		jsonResponse(w, followPayload(db, viewer, target))
	}
}

// handleToggleFollow follows or unfollows a profile as the viewer's own.
func handleToggleFollow(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		authorID, ok := requireAuthor(db, w, user)
		if !ok {
			return
		}
		var in struct {
			AuthorID string `json:"profile_id"`
			Slug     string `json:"slug"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		target := resolveAuthor(db, strings.TrimSpace(in.AuthorID), strings.TrimSpace(in.Slug))
		if target == "" {
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		}
		if target == authorID {
			jsonError(w, data.ErrSelfFollow.Error(), http.StatusBadRequest)
			return
		}
		if !db.RateLimitAllow("follow:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "you're following too fast — slow down", http.StatusTooManyRequests)
			return
		}
		following, err := db.ToggleFollow(authorID, target)
		if errors.Is(err, data.ErrSelfFollow) {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("follow error: %v", err), http.StatusInternalServerError)
			return
		}
		if following {
			notifyFollow(db, target, authorID)
		}
		jsonResponse(w, followPayload(db, authorID, target))
	}
}

// notifyFollow tells a profile someone started following them.
func notifyFollow(db *data.DB, followeeID, followerID string) {
	db.Notify(followeeID, data.NotifyFollow, "author", followeeID, followerID)
}

// handleUnfollow removes the viewer's follow of a profile (idempotent).
func handleUnfollow(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authorID, ok := requireAuthor(db, w, authFunc(r))
		if !ok {
			return
		}
		if err := db.Unfollow(authorID, chi.URLParam(r, "profile_id")); err != nil {
			jsonError(w, fmt.Sprintf("unfollow error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleProfileFollows lists who follows a profile (which = "followers") or who
// they follow (which = "following"). Profile visibility applies.
func handleProfileFollows(db *data.DB, authFunc func(*http.Request) *data.User, which string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !profilesGate(db, w, authFunc(r)) {
			return
		}
		profile, err := db.ProfileBySlug(chi.URLParam(r, "slug"))
		if err == sql.ErrNoRows {
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		id := profile["id"].(string)
		var list []map[string]any
		if which == "followers" {
			list, err = db.FollowerProfiles(id)
		} else {
			list, err = db.FollowingProfiles(id)
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"profiles": list, which: len(list)})
	}
}
