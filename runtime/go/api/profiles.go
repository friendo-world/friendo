package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Profiles API (v0.6 Tier A).
//
//	GET  /_/api/profiles            every profile on the site (+ the declared fields)
//	GET  /_/api/profiles/{slug}     one profile with its posts and comments
//	PUT  /_/api/me/profiles/{id}    edit one of your profiles: name, avatar, bio, slug, data
//	DELETE /_/api/me/profiles/{id}  remove one of your profiles (never the last)
//
// Who may read follows the site's profile_visibility setting: "members" (the
// default) needs a signed-in viewer, "public" doesn't. Writes are the profile
// owner's alone. Profiles are profiles, so the switch that turns them off is
// the site's own choice of visibility — there's no feature switch to refuse them.

// profileReserved are the profile's own fields, which a declared field may not shadow.
var profileReserved = map[string]bool{
	"id": true, "name": true, "slug": true, "avatar": true, "bio": true, "email": true, "role": true,
	"url": true, "created": true, "updated": true, "user_id": true, "fields": true,
}

// loadProfileFields reads `[profiles] fields` from friendo.toml — the same grammar
// as [content.<type>.fields] — so the admin's Members screen, <friendo-profile>
// and the profile editor lay themselves out from it.
//
//	[profiles.fields]
//	pronouns = "text"
//	website  = { kind = "text", hint = "https://…" }
func loadProfileFields(siteDir string) []FieldDecl {
	out := []FieldDecl{}
	if siteDir == "" {
		return out
	}
	path := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(path); err != nil {
		return out
	}
	var raw struct {
		Profiles struct {
			Fields map[string]toml.Primitive `toml:"fields"`
		} `toml:"profiles"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		log.Printf("friendo.toml: could not read [profiles]: %v", err)
		return out
	}
	// Field order is the order the keys appear in the file.
	for _, k := range md.Keys() {
		if len(k) != 3 || k[0] != "profiles" || k[1] != "fields" {
			continue
		}
		prim, ok := raw.Profiles.Fields[k[2]]
		if !ok {
			continue
		}
		f, ok := parseField(md, "profiles", k[2], prim)
		if !ok {
			continue
		}
		if profileReserved[f.Name] {
			log.Printf("friendo.toml: [profiles.fields] %s: that name is one of a profile's own fields", f.Name)
			continue
		}
		out = append(out, f)
	}
	return out
}

// profilesGate answers 401 when the site keeps profiles for members and nobody is
// signed in. Returns false after writing the error.
func profilesGate(db *data.DB, w http.ResponseWriter, user *data.User) bool {
	if db.ProfilesVisibleTo(user != nil) {
		return true
	}
	jsonError(w, "sign in to see profiles", http.StatusUnauthorized)
	return false
}

// handleListProfiles lists every profile on the site.
func handleListProfiles(db *data.DB, authFunc func(*http.Request) *data.User, fields []FieldDecl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !profilesGate(db, w, authFunc(r)) {
			return
		}
		profiles, err := db.ListProfiles()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"profiles": profiles, "fields": fields, "visibility": db.ProfileVisibility()})
	}
}

// handleGetProfile returns one profile by address, with its published posts and
// approved comments — what pages/profiles/[slug].html renders server-side.
func handleGetProfile(db *data.DB, authFunc func(*http.Request) *data.User, fields []FieldDecl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if !profilesGate(db, w, user) {
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
		AttachProfileRelations(db, profile, time.Now(), true)
		out := map[string]any{"profile": profile, "fields": fields}
		if user != nil {
			// The viewer's own profile is editable in place.
			out["mine"] = db.UserOwnsAuthor(user.ID, profile["id"].(string))
		}
		jsonResponse(w, out)
	}
}

// AttachProfileRelations adds what a profile page shows beside the profile: its
// published posts (with when/location/author attached like any listing) and its
// approved comments (empty when comments are off). Shared by the page render, the
// API and the static export so a profile looks the same everywhere; forAPI picks
// the JSON shape of `when` over the template one (which carries the series).
func AttachProfileRelations(db *data.DB, profile map[string]any, now time.Time, forAPI bool) {
	id, _ := profile["id"].(string)
	posts, err := db.PostsByAuthor(id)
	if err != nil {
		posts = []map[string]any{}
	}
	db.AttachWhen(posts, now, forAPI)
	db.AttachLocations(posts)
	db.AttachAuthors(posts)
	profile["posts"] = posts
	profile["comments"] = []map[string]any{}
	if db.FeatureOn("comments") {
		if comments, err := db.CommentsByAuthor(id); err == nil {
			profile["comments"] = comments
		}
	}
	// Who follows them and who they follow ({{ record.followers }}, {{ record.follower_count }}).
	profile["followers"], profile["following"] = []map[string]any{}, []map[string]any{}
	profile["follower_count"], profile["following_count"] = 0, 0
	if db.FeatureOn("follows") {
		if list, err := db.FollowerProfiles(id); err == nil {
			profile["followers"] = list
			profile["follower_count"] = len(list)
		}
		if list, err := db.FollowingProfiles(id); err == nil {
			profile["following"] = list
			profile["following_count"] = len(list)
		}
	}
}

// profileInput is the body of PUT /me/profiles/{id}. Pointers tell "leave it" from
// "clear it"; data replaces the profile's declared fields when present.
type profileInput struct {
	Name   *string        `json:"name"`
	Avatar *string        `json:"avatar"`
	Bio    *string        `json:"bio"`
	Slug   *string        `json:"slug"`
	Data   map[string]any `json:"fields"`
}

// handleUpdateMyProfile edits one of the caller's profiles.
func handleUpdateMyProfile(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in profileInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}
		if in.Bio != nil && len(*in.Bio) > 2000 {
			jsonError(w, "bio is too long (2000 characters at most)", http.StatusBadRequest)
			return
		}
		// A profile's own fields live in columns; drop them from data so a client
		// can't shadow the name or slug through the blob.
		for k := range in.Data {
			if profileReserved[k] {
				delete(in.Data, k)
			}
		}
		profile, err := db.UpdateProfile(user.ID, chi.URLParam(r, "id"), data.ProfileUpdate{
			Name: in.Name, Avatar: in.Avatar, Bio: in.Bio, Slug: in.Slug, Data: in.Data,
		})
		switch {
		case err == sql.ErrNoRows:
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		case errors.Is(err, data.ErrSlugTaken):
			jsonError(w, err.Error(), http.StatusConflict)
			return
		case err != nil:
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"profile": profile})
	}
}

// handleDeleteProfile removes one of the caller's profiles (never the last).
func handleDeleteProfile(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		err := db.DeleteProfile(user.ID, chi.URLParam(r, "id"))
		switch {
		case err == sql.ErrNoRows:
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		case errors.Is(err, data.ErrLastProfile):
			jsonError(w, err.Error(), http.StatusConflict)
			return
		case err != nil:
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
