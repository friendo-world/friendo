package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Groups API (v0.6 Tier C). Every route is behind the `groups` switch. {id} is
// a group's post id or its slug.
//
//	GET    /_/api/groups                          the groups the viewer may see (+ can_create)
//	GET    /_/api/groups/{id}                     one group with members and the viewer's standing
//	GET    /_/api/groups/{id}/members?status=     members (moderators also see requested)
//	POST   /_/api/groups/{id}/join                join (open) or ask (request); invite-only refuses
//	DELETE /_/api/groups/{id}/join                leave, or withdraw a request
//	POST   /_/api/groups/{id}/members             admin adds {profile_id | slug}
//	PUT    /_/api/groups/{id}/members/{profile_id} moderator approves {status}; admin promotes {role}
//	DELETE /_/api/groups/{id}/members/{profile_id} moderator removes
//	PUT    /_/api/groups/{id}/settings            admin sets {visibility, join}
//
// A group mirrors the site: site admins are admins of every group, and site
// moderators and up (review.any) moderate every group.
//
// A group is created like any post: POST /collections/groups/records — see the
// groups hook in handleCreateRecord (members may when members_can_start_groups is on).

// resolveGroup finds a published group by id or slug.
func resolveGroup(db *data.DB, idOrSlug string) (map[string]any, error) {
	if g, err := db.GroupByID(idOrSlug); err == nil {
		return g, nil
	}
	return db.GroupBySlug(idOrSlug)
}

// groupViewer is the viewer as the group rules see them.
type groupViewer struct {
	user     *data.User
	authorID string
	memberOf map[string]bool
}

func groupViewerFor(db *data.DB, user *data.User) groupViewer {
	v := groupViewer{user: user, memberOf: map[string]bool{}}
	if user != nil {
		v.authorID = db.DefaultAuthorID(user.ID)
		v.memberOf = db.MemberOfSet(v.authorID)
	}
	return v
}

// moderates reports whether the viewer keeps a group tidy: its moderators and
// admins, and the site's moderators and up (review.any).
func (v groupViewer) moderates(db *data.DB, groupID string) bool {
	if v.user == nil {
		return false
	}
	return v.user.Can(data.CapReviewAny) || db.IsGroupModerator(groupID, v.authorID)
}

// administers reports whether the viewer runs a group: its admins, and the
// site's admins and owners (site.configure).
func (v groupViewer) administers(db *data.DB, groupID string) bool {
	if v.user == nil {
		return false
	}
	return v.user.Can(data.CapSiteConfigure) || db.IsGroupAdmin(groupID, v.authorID)
}

// loadGroup resolves {id}, applies visibility (401 signed out, 404 otherwise)
// and returns the record. false after writing the error.
func loadGroup(db *data.DB, w http.ResponseWriter, r *http.Request, v groupViewer) (map[string]any, bool) {
	g, err := resolveGroup(db, chi.URLParam(r, "id"))
	if err == sql.ErrNoRows {
		jsonError(w, "group not found", http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
		return nil, false
	}
	if !data.CanSeeGroup(g, v.user != nil, v.memberOf) {
		if v.user == nil {
			jsonError(w, "sign in to see this group", http.StatusUnauthorized)
		} else {
			jsonError(w, "group not found", http.StatusNotFound)
		}
		return nil, false
	}
	return g, true
}

// groupJSON is a group as the API shows it: the record plus settings, member
// count, the viewer's standing, and its page.
func groupJSON(db *data.DB, g map[string]any, v groupViewer, permalink data.PermalinkFunc) map[string]any {
	id, _ := g["id"].(string)
	visibility, join := data.GroupSettings(g)
	out := map[string]any{
		"id": id, "slug": g["slug"], "title": g["title"], "body": g["body"], "author_id": g["author_id"],
		"fields": g["fields"], "created": g["created"], "updated": g["updated"],
		"settings":     map[string]any{"visibility": visibility, "join": join},
		"member_count": db.MemberCount(id),
		"chats":        []map[string]any{},
		"url":          "",
		"mine":         nil,
		"moderates":    v.moderates(db, id),
		"administers":  v.administers(db, id),
	}
	slug, _ := g["slug"].(string)
	if permalink != nil {
		out["url"] = permalink(data.GroupsCollection, map[string]string{"slug": slug, "id": id})
	}
	if out["url"] == "" {
		out["url"] = "/groups/" + slug
	}
	if v.authorID != "" {
		if role, status, ok := db.Membership(id, v.authorID); ok {
			out["mine"] = map[string]any{"role": role, "status": status}
		}
	}
	// The group's chats (names aren't secret; the messages are).
	if db.FeatureOn("chats") {
		if chats, err := db.ListGroupChats(id); err == nil {
			out["chats"] = chats
		}
	}
	return out
}

// canCreateGroup: content.create holders always; members when members_can_start_groups is on.
func canCreateGroup(db *data.DB, user *data.User) bool {
	if user == nil {
		return false
	}
	return user.Can(data.CapContentCreate) || db.MembersCanStartGroups()
}

func handleListGroups(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := groupViewerFor(db, authFunc(r))
		records, err := db.QueryPublishedCollection(data.GroupsCollection)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		out := []map[string]any{}
		for _, g := range records {
			if data.CanSeeGroup(g, v.user != nil, v.memberOf) {
				out = append(out, groupJSON(db, g, v, permalink))
			}
		}
		jsonResponse(w, map[string]any{"groups": out, "can_create": canCreateGroup(db, v.user)})
	}
}

func handleGetGroup(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := groupViewerFor(db, authFunc(r))
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		out := groupJSON(db, g, v, permalink)
		id := g["id"].(string)
		members, _ := db.ListMembers(id, "")
		out["members"] = members
		if v.moderates(db, id) {
			requested, _ := db.ListMembers(id, data.MembershipRequested)
			out["requested"] = requested
		}
		jsonResponse(w, map[string]any{"group": out})
	}
}

func handleListGroupMembers(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := groupViewerFor(db, authFunc(r))
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		status := r.URL.Query().Get("status")
		if status != "" && status != data.MembershipMember && !v.moderates(db, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		members, err := db.ListMembers(id, status)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"members": members})
	}
}

// handleJoinGroup: the viewer joins (open), asks (request), or is refused (invite).
func handleJoinGroup(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		authorID, ok := requireAuthor(db, w, user)
		if !ok {
			return
		}
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if _, status, has := db.Membership(id, authorID); has && status == data.MembershipMember {
			jsonResponse(w, map[string]any{"group": groupJSON(db, g, v, permalink)})
			return
		}
		if !db.RateLimitAllow("group:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "you're joining too fast — slow down", http.StatusTooManyRequests)
			return
		}
		_, join := data.GroupSettings(g)
		switch join {
		case "open":
			db.SetMembership(id, authorID, data.GroupRoleMember, data.MembershipMember)
		case "request":
			// An invitation waiting for them is an approval already.
			if _, status, has := db.Membership(id, authorID); has && status == data.MembershipInvited {
				db.SetMembership(id, authorID, data.GroupRoleMember, data.MembershipMember)
			} else {
				db.SetMembership(id, authorID, data.GroupRoleMember, data.MembershipRequested)
				db.NotifyMany(db.GroupModerators(id), data.NotifyJoinRequest, "post", id, authorID)
			}
		default: // invite
			if _, status, has := db.Membership(id, authorID); has && status == data.MembershipInvited {
				db.SetMembership(id, authorID, data.GroupRoleMember, data.MembershipMember)
			} else {
				jsonError(w, "this group is invite only", http.StatusForbidden)
				return
			}
		}
		jsonResponse(w, map[string]any{"group": groupJSON(db, g, v, permalink)})
	}
}

// handleLeaveGroup: leave, or withdraw a request. The last admin can't leave.
func handleLeaveGroup(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		authorID, ok := requireAuthor(db, w, user)
		if !ok {
			return
		}
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if db.IsGroupAdmin(id, authorID) && len(db.GroupAdmins(id)) == 1 {
			jsonError(w, "make someone else an admin before leaving", http.StatusConflict)
			return
		}
		db.RemoveMembership(id, authorID)
		jsonResponse(w, map[string]any{"group": groupJSON(db, g, v, permalink)})
	}
}

// handleAddGroupMember: an admin adds someone straight in (status member).
func handleAddGroupMember(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if !v.administers(db, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		var in struct {
			AuthorID string `json:"profile_id"`
			Slug     string `json:"slug"`
			Role     string `json:"role"`
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
		if err := db.SetMembership(id, target, in.Role, data.MembershipMember); err != nil {
			jsonError(w, fmt.Sprintf("membership error: %v", err), http.StatusInternalServerError)
			return
		}
		db.Notify(target, data.NotifyMembership, "post", id, v.authorID)
		members, _ := db.ListMembers(id, "")
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"members": members})
	}
}

// handleSetGroupMember: a moderator approves a request (status → member); an
// admin changes a role.
func handleSetGroupMember(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if !v.moderates(db, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		target := chi.URLParam(r, "profile_id")
		role, status, has := db.Membership(id, target)
		if !has {
			jsonError(w, "not a member", http.StatusNotFound)
			return
		}
		var in struct {
			Status *string `json:"status"`
			Role   *string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		wasPending := status != data.MembershipMember
		if in.Role != nil && !v.administers(db, id) {
			jsonError(w, "only a group's admins change roles", http.StatusForbidden)
			return
		}
		if in.Status != nil {
			if *in.Status != data.MembershipMember && *in.Status != data.MembershipRequested && *in.Status != data.MembershipInvited {
				jsonError(w, "status must be member, requested or invited", http.StatusBadRequest)
				return
			}
			status = *in.Status
		}
		if in.Role != nil {
			if !data.ValidGroupRole(*in.Role) {
				jsonError(w, "role must be admin, moderator or member", http.StatusBadRequest)
				return
			}
			if role == data.GroupRoleAdmin && *in.Role != role && len(db.GroupAdmins(id)) == 1 {
				jsonError(w, "a group keeps at least one admin", http.StatusConflict)
				return
			}
			role = *in.Role
		}
		if err := db.SetMembership(id, target, role, status); err != nil {
			jsonError(w, fmt.Sprintf("membership error: %v", err), http.StatusInternalServerError)
			return
		}
		if wasPending && status == data.MembershipMember {
			db.Notify(target, data.NotifyMembership, "post", id, v.authorID)
		}
		members, _ := db.ListMembers(id, "")
		requested, _ := db.ListMembers(id, data.MembershipRequested)
		jsonResponse(w, map[string]any{"members": members, "requested": requested})
	}
}

// handleRemoveGroupMember: a moderator removes someone (or declines a request).
func handleRemoveGroupMember(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if !v.moderates(db, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		target := chi.URLParam(r, "profile_id")
		if db.IsGroupAdmin(id, target) && len(db.GroupAdmins(id)) == 1 {
			jsonError(w, "a group keeps at least one admin", http.StatusConflict)
			return
		}
		if db.IsGroupAdmin(id, target) && !v.administers(db, id) {
			jsonError(w, "only a group's admins remove an admin", http.StatusForbidden)
			return
		}
		if err := db.RemoveMembership(id, target); err != nil {
			jsonError(w, fmt.Sprintf("membership error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleGroupSettings: an admin sets who can see the group and how people join.
func handleGroupSettings(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		v := groupViewerFor(db, user)
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		id := g["id"].(string)
		if !v.administers(db, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		var in struct {
			Visibility *string `json:"visibility"`
			Join       *string `json:"join"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		d, _ := g["fields"].(map[string]any)
		if d == nil {
			d = map[string]any{}
		}
		if in.Visibility != nil {
			if !data.GroupVisibilities[*in.Visibility] {
				jsonError(w, "visibility must be public, members or private", http.StatusBadRequest)
				return
			}
			d["visibility"] = *in.Visibility
		}
		if in.Join != nil {
			if !data.GroupJoins[*in.Join] {
				jsonError(w, "join must be open, request or invite", http.StatusBadRequest)
				return
			}
			d["join"] = *in.Join
		}
		b, _ := json.Marshal(d)
		if err := db.SetRecordData(id, string(b)); err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		g, _ = db.GroupByID(id)
		jsonResponse(w, map[string]any{"group": groupJSON(db, g, v, permalink)})
	}
}

// withGroupFields gives a declared `groups` type its two built-in settings as
// declared fields, so the admin's form offers them as menus without the site
// writing them down. A site that declares them itself keeps its own.
func withGroupFields(types []ContentType) []ContentType {
	for i, t := range types {
		if t.Name != data.GroupsCollection {
			continue
		}
		have := map[string]bool{}
		for _, f := range t.Fields {
			have[f.Name] = true
		}
		builtin := []FieldDecl{}
		if !have["visibility"] {
			builtin = append(builtin, FieldDecl{Name: "visibility", Kind: "text", Choices: []string{"public", "members", "private"}, Hint: "Who can see the group: everyone, signed-in members, or its members only"})
		}
		if !have["join"] {
			builtin = append(builtin, FieldDecl{Name: "join", Kind: "text", Choices: []string{"open", "request", "invite"}, Hint: "How people get in: join at once, ask a moderator, or be added by an admin"})
		}
		types[i].Fields = append(builtin, t.Fields...)
	}
	return types
}
