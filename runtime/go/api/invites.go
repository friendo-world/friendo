package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Event invitations (v0.6 Tier E).
//
//	POST /_/api/posts/{id}/invites  {slugs: […], profile_ids: […], group: "board", followers: true, date: ""}
//
// The organizer — the event's author, or a moderator — names who to ask: profile
// addresses, a group (its members), or "followers" (everyone following the
// organizer). Each becomes an RSVP row with answer "invited" for the occurrence
// (the next one unless named), plus a notification. Someone who already answered
// is left alone and counted as skipped. Behind the rsvp switch, like every RSVP call.

func handleInviteToEvent(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		organizer, ok := requireAuthor(db, w, user)
		if !ok {
			return
		}
		var in struct {
			Slugs      []string `json:"slugs"`
			AuthorIDs  []string `json:"profile_ids"`
			Group      string   `json:"group"`
			Followers  bool     `json:"followers"`
			Occurrence string   `json:"date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		ev, key, ok := eventForPost(w, db, chi.URLParam(r, "id"), in.Occurrence)
		if !ok {
			return
		}
		if !user.Can(data.CapReviewAny) && !db.UserOwnsEventPost(user.ID, ev) {
			jsonError(w, "only the event's organizer can invite people", http.StatusForbidden)
			return
		}
		if !db.RateLimitAllow("invite:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "you're inviting too fast — slow down", http.StatusTooManyRequests)
			return
		}

		// Who to ask, deduplicated, never the organizer.
		want := []string{}
		seen := map[string]bool{organizer: true}
		add := func(id string) {
			if id != "" && !seen[id] {
				seen[id] = true
				want = append(want, id)
			}
		}
		unknown := []string{}
		for _, s := range in.Slugs {
			if id := resolveAuthor(db, "", strings.TrimSpace(s)); id != "" {
				add(id)
			} else if strings.TrimSpace(s) != "" {
				unknown = append(unknown, strings.TrimSpace(s))
			}
		}
		for _, id := range in.AuthorIDs {
			if resolved := resolveAuthor(db, strings.TrimSpace(id), ""); resolved != "" {
				add(resolved)
			}
		}
		if g := strings.TrimSpace(in.Group); g != "" {
			group, err := resolveGroup(db, g)
			if err != nil {
				jsonError(w, "group not found", http.StatusNotFound)
				return
			}
			if !db.FeatureOn("groups") {
				jsonError(w, "groups are turned off for this site", http.StatusForbidden)
				return
			}
			members, _ := db.ListMembers(group["id"].(string), "")
			for _, m := range members {
				add(m["id"].(string))
			}
		}
		if in.Followers {
			if !db.FeatureOn("follows") {
				jsonError(w, "follows are turned off for this site", http.StatusForbidden)
				return
			}
			for _, id := range db.Followers(organizer) {
				add(id)
			}
		}

		// An anonymous event's organizer invites without their name on it.
		from := organizer
		if db.PostIsAnonymous(ev.TargetID) {
			from = ""
		}
		invited, skipped := 0, 0
		for _, id := range want {
			wrote, err := db.InviteRSVP(ev.ID, key, id)
			if err != nil {
				jsonError(w, fmt.Sprintf("invite error: %v", err), http.StatusInternalServerError)
				return
			}
			if !wrote {
				skipped++
				continue
			}
			invited++
			db.Notify(id, data.NotifyEventInvite, "post", ev.TargetID, from)
		}
		out := rsvpPayload(db, user, ev, key)
		out["invited"], out["skipped"], out["unknown"] = invited, skipped, unknown
		jsonResponse(w, out)
	}
}
