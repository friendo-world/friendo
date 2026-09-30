package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/email"
	"github.com/friendo-world/friendo/runtime/go/storage"
)

// Mount registers the REST + sync API routes under /api on the given router,
// which is expected to be the /_ subrouter (so endpoints live at /_/api/*).
//
// Public endpoints (me, auth) carry no auth so the SPA can bootstrap and log in.
// Everything else requires site admin authentication (session cookie).
//
// onTemplatesChanged (optional) runs after every templates push: the server
// uses it to rebuild its route table, so a page pushed after the site was first
// served starts routing at once.
func Mount(r chi.Router, db *data.DB, siteDir, siteName string, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc, store storage.Backend, onTemplatesChanged func()) {
	// friendo.toml's [settings] block is the source of truth for the settings it
	// declares: apply them to the DB now (overwriting any admin edit) and mark them
	// so the settings API reports them read-only and refuses to change them.
	managed := applyManagedSettings(db, siteDir)
	// [content] collections and their fields, read once; the admin lays itself out from them.
	contentTypes, typesDeclared := loadContentTypes(siteDir)
	contentTypes = withGroupFields(contentTypes)
	// [profiles] fields — what a profile carries beyond name, avatar and bio.
	profileFields := loadProfileFields(siteDir)
	// viewer also knows a visitor who already has a session — for public reads
	// that say what's yours. Never used to decide what someone may do.
	viewer := viewerFunc(db, authFunc)
	r.Route("/api", func(r chi.Router) {
		// Public endpoints — used by the SPA to bootstrap and authenticate.
		r.Get("/me", handleMe(db, authFunc))
		// Which community features are on (Settings → Features).
		r.Get("/features", handleFeatures(db))
		// What a visitor (not signed in) may do here, and who this browser is.
		r.Get("/visitor", handleVisitor(db, authFunc))

		// Profiles: an account's author profiles. Any authenticated member may
		// list/create their own and pick which one their comments/messages use.
		r.Get("/me/profiles", handleListProfilesFor(db, authFunc))
		r.Post("/me/profiles", handleCreateProfile(db, authFunc))
		r.Post("/me/profiles/{id}/default", handleSetDefaultProfile(db, authFunc))
		r.Put("/me/profiles/{id}", handleUpdateMyProfile(db, authFunc))
		// The member's private calendar link (see calendar.go).
		r.Get("/me/calendar", handleMyCalendar(db, authFunc))
		r.Post("/me/calendar/reset", handleResetMyCalendar(db, authFunc))
		r.Delete("/me/profiles/{id}", handleDeleteProfile(db, authFunc))
		// Profiles: every profile has an address. Who may read follows the site's
		// profile_visibility setting (members by default).
		r.Get("/profiles", handleListProfiles(db, authFunc, profileFields))
		r.Get("/profiles/{slug}", handleGetProfile(db, authFunc, profileFields))
		// Follows: one-way; a friend is a mutual follow. Public counts, member writes.
		r.Get("/follows", featureGate(db, "follows", handleFollowStatus(db, authFunc)))
		r.Post("/follows", featureGate(db, "follows", handleToggleFollow(db, authFunc)))
		r.Delete("/follows/{profile_id}", featureGate(db, "follows", handleUnfollow(db, authFunc)))
		r.Get("/profiles/{slug}/followers", featureGate(db, "follows", handleProfileFollows(db, authFunc, "followers")))
		r.Get("/profiles/{slug}/following", featureGate(db, "follows", handleProfileFollows(db, authFunc, "following")))
		// Groups: a post in the `groups` collection with members. Public reads
		// follow each group's visibility; joining is a member act; moderators run it.
		r.Get("/groups", featureGate(db, "groups", handleListGroups(db, authFunc, permalink)))
		r.Get("/groups/{id}", featureGate(db, "groups", handleGetGroup(db, authFunc, permalink)))
		r.Get("/groups/{id}/members", featureGate(db, "groups", handleListGroupMembers(db, authFunc)))
		r.Post("/groups/{id}/join", featureGate(db, "groups", handleJoinGroup(db, authFunc, permalink)))
		r.Delete("/groups/{id}/join", featureGate(db, "groups", handleLeaveGroup(db, authFunc, permalink)))
		r.Post("/groups/{id}/members", featureGate(db, "groups", handleAddGroupMember(db, authFunc)))
		r.Put("/groups/{id}/members/{profile_id}", featureGate(db, "groups", handleSetGroupMember(db, authFunc)))
		r.Delete("/groups/{id}/members/{profile_id}", featureGate(db, "groups", handleRemoveGroupMember(db, authFunc)))
		r.Put("/groups/{id}/settings", featureGate(db, "groups", handleGroupSettings(db, authFunc, permalink)))
		// The inbox: what happened to you, read when you visit. On while follows or groups is.
		r.Get("/me/notifications", notificationsGate(db, handleListNotifications(db, authFunc, permalink)))
		r.Put("/me/notifications/read-all", notificationsGate(db, handleMarkAllNotificationsRead(db, authFunc)))
		r.Put("/me/notifications/{id}/read", notificationsGate(db, handleMarkNotificationRead(db, authFunc)))
		r.Post("/auth/login", handleLogin(db))
		r.Post("/auth/logout", handleLogout(db))
		r.Get("/setup", handleSetupStatus(db))
		r.Post("/setup/request-code", handleSetupRequestCode(db))
		r.Post("/setup", handleSetupCreate(db))
		r.Post("/migrate", handleMigrate(db))
		r.Post("/auth/request-code", handleRequestCode(db))
		r.Post("/auth/verify-code", handleVerifyCode(db))

		// Community: public reads, member-gated writes. These self-gate rather
		// than joining the admin group — reads are open, and a write needs only
		// an authenticated member (any role), not admin.
		r.Get("/posts/{id}/comments", featureGate(db, "comments", handleListComments(db, authFunc)))
		r.Post("/posts/{id}/comments", featureGate(db, "comments", handlePostComment(db, authFunc)))
		r.Get("/reactions", featureGate(db, "reactions", handleListReactions(db, viewer)))
		r.Post("/reactions", featureGate(db, "reactions", visitorsMay(db, authFunc, "react", func(a authFn) http.HandlerFunc { return handleToggleReaction(db, a) })))

		// Chats & messages (community feed): public reads, member-gated posts.
		r.Get("/chats", featureGate(db, "chats", handleListChats(db)))
		r.Get("/chats/{id}/messages", featureGate(db, "chats", handleListMessages(db, authFunc)))
		r.Get("/chats/{id}/stream", featureGate(db, "chats", handleChatStream(db))) // SSE: live new messages
		r.Post("/chats/{id}/messages", featureGate(db, "chats", handlePostMessage(db, authFunc)))
		r.Delete("/messages/{id}", featureGate(db, "chats", handleDeleteMessage(db, authFunc)))
		// A group's chats: its members' rooms (see group_chats.go).
		r.Get("/groups/{id}/chats", featureGate(db, "chats", featureGate(db, "groups", handleListGroupChats(db, authFunc))))
		r.Post("/groups/{id}/chats", featureGate(db, "chats", featureGate(db, "groups", handleCreateGroupChat(db, authFunc))))
		r.Delete("/groups/{id}/chats/{chat}", featureGate(db, "chats", featureGate(db, "groups", handleDeleteGroupChat(db, authFunc))))
		r.Get("/groups/{id}/chats/{chat}/messages", featureGate(db, "chats", featureGate(db, "groups", handleListGroupChatMessages(db, authFunc))))
		r.Post("/groups/{id}/chats/{chat}/messages", featureGate(db, "chats", featureGate(db, "groups", handlePostGroupChatMessage(db, authFunc))))
		r.Get("/groups/{id}/chats/{chat}/stream", featureGate(db, "chats", featureGate(db, "groups", handleGroupChatStream(db, authFunc))))
		// RSVP: public counts; a signed-in member answers (going / not_going /
		// maybe) for one date of a post's event. Names for organizers only.
		// A visitor may answer too when visitors_can_rsvp is on (see visitors.go).
		r.Get("/posts/{id}/rsvps", featureGate(db, "rsvp", handleGetRSVPs(db, viewer)))
		r.Post("/posts/{id}/rsvps", featureGate(db, "rsvp", visitorsMay(db, authFunc, "rsvp", func(a authFn) http.HandlerFunc { return handleSetRSVP(db, a) })))
		r.Delete("/posts/{id}/rsvps", featureGate(db, "rsvp", handleDeleteRSVP(db, viewer)))
		r.Get("/posts/{id}/rsvps/names", featureGate(db, "rsvp", handleRSVPNames(db, authFunc)))
		// Invitations: the organizer asks people (by address, group, or followers);
		// each becomes an RSVP row awaiting an answer, plus a notification.
		r.Post("/posts/{id}/invites", featureGate(db, "rsvp", handleInviteToEvent(db, authFunc)))
		r.Get("/polls/by-slug/{slug}", featureGate(db, "polls", handleGetPollBySlug(db, viewer)))
		r.Get("/polls/{id}", featureGate(db, "polls", handleGetPoll(db, viewer)))
		r.Post("/polls/{id}/vote", featureGate(db, "polls", visitorsMay(db, authFunc, "vote", func(a authFn) http.HandlerFunc { return handleVotePoll(db, a) })))

		// Locations (geo-tagging): public reads; contributor+ attaches/removes
		// (own posts) or editor+ (any). Ownership is enforced inside the handler,
		// so the route gate is the lower content.edit.own capability.
		r.Get("/locations", featureGate(db, "locations", handleListLocations(db, permalink)))

		// Files (per-record media): public reads; contributor+ uploads/removes.
		r.Get("/files", handleListFiles(db))

		// Content — contributor+ (holds content.create). The handlers scope to the
		// actor's own posts unless they also hold content.edit.any.
		r.Get("/collections", capGate(authFunc, data.CapContentCreate, handleListCollections(db, contentTypes, typesDeclared, profileFields)))
		// The [content] block that matches the site as it is (to copy, or for `friendo pull`).
		r.Get("/content/toml", capGate(authFunc, data.CapSiteConfigure, handleContentToml(db, contentTypes)))
		r.Get("/collections/{collection}/posts", capGate(authFunc, data.CapContentCreate, handleListRecords(db, authFunc)))
		// Create self-gates: contributors+ (content.create) post directly; when the
		// site opts in (content.accept_submissions), a signed-in member may submit a
		// post that's forced into the pending review queue.
		r.Post("/collections/{collection}/posts", handleCreateRecord(db, authFunc))
		// The review queue — moderator+ (review.posts) lists posts by status and
		// approves or rejects a pending one; content.publish (editor+) may set any
		// status. Neither touches the post's content.
		r.Get("/posts", capGate(authFunc, data.CapReviewPosts, handleListRecordsByStatus(db)))
		r.Get("/posts/{id}", capGate(authFunc, data.CapContentCreate, handleGetRecord(db, authFunc)))
		r.Put("/posts/{id}/status", capGate(authFunc, data.CapReviewPosts, handleSetRecordStatus(db, authFunc)))
		r.Put("/posts/{id}", capGate(authFunc, data.CapContentCreate, handleUpdateRecord(db, authFunc)))
		r.Delete("/posts/{id}", capGate(authFunc, data.CapContentCreate, handleDeleteRecord(db, authFunc)))

		// Comment review — contributor+ (review.own); scoped to own posts
		// unless the actor holds review.any.
		r.Get("/comments", featureGate(db, "comments", capGate(authFunc, data.CapReviewOwn, handleModerationList(db, authFunc))))
		r.Put("/comments/{id}", featureGate(db, "comments", capGate(authFunc, data.CapReviewOwn, handleUpdateComment(db, authFunc))))
		// Delete self-gates: a member may delete their own comment; moderators
		// may delete comments they're allowed to moderate.
		r.Delete("/comments/{id}", featureGate(db, "comments", handleDeleteComment(db, authFunc)))

		// Poll creation — editor+ (content.edit.any).
		r.Post("/polls", featureGate(db, "polls", capGate(authFunc, data.CapContentEditAny, handleCreatePoll(db))))

		// Location tagging — contributor+ (content.edit.own). The gate admits
		// contributors; the handler then requires the actor to own the target post
		// unless they also hold content.edit.any (editor+), mirroring the record API.
		r.Post("/locations", featureGate(db, "locations", capGate(authFunc, data.CapContentEditOwn, handleCreateLocation(db, authFunc))))
		r.Delete("/locations/{id}", featureGate(db, "locations", capGate(authFunc, data.CapContentEditOwn, handleDeleteLocation(db, authFunc))))

		// Media upload — contributor+ (content.create). Bytes land in assets/ and
		// are served by the static /assets/* handler; the row links them to a record.
		r.Post("/files", capGate(authFunc, data.CapContentCreate, handleUploadFile(db, siteDir, store)))
		r.Delete("/files/{id}", capGate(authFunc, data.CapContentCreate, handleDeleteFile(db, siteDir, store)))

		// Chat management — admin+ (site.configure); posting is member-gated above.
		r.Post("/chats", featureGate(db, "chats", capGate(authFunc, data.CapSiteConfigure, handleCreateChat(db))))
		r.Delete("/chats/{id}", featureGate(db, "chats", capGate(authFunc, data.CapSiteConfigure, handleDeleteChat(db))))

		// Users — admin+ (user.manage). Granting admin/owner additionally needs
		// site.own (enforced by canAssignRole).
		r.Get("/users", capGate(authFunc, data.CapUserManage, handleListUsers(db)))
		r.Post("/users", capGate(authFunc, data.CapUserManage, handleCreateUser(db, authFunc)))
		r.Put("/users/{id}", capGate(authFunc, data.CapUserManage, handleUpdateUser(db, authFunc)))
		r.Delete("/users/{id}", capGate(authFunc, data.CapUserManage, handleDeleteUser(db, authFunc)))

		// Settings + sync — admin+ (site.configure).
		r.Get("/settings", capGate(authFunc, data.CapSiteConfigure, handleSettings(db, siteName, managed, typesDeclared)))
		r.Put("/settings", capGate(authFunc, data.CapSiteConfigure, handleUpdateSettings(db, siteName, managed, typesDeclared)))
		r.Post("/push/templates", capGate(authFunc, data.CapSiteConfigure, handlePushTemplates(siteDir, onTemplatesChanged)))
		r.Post("/push/assets", capGate(authFunc, data.CapSiteConfigure, handlePushAssets(siteDir, store)))
		r.Post("/push/data", capGate(authFunc, data.CapSiteConfigure, handlePushData(db)))
		r.Post("/push/users", capGate(authFunc, data.CapSiteConfigure, handlePushUsers(db)))
		r.Post("/push/settings", capGate(authFunc, data.CapSiteConfigure, handlePushSettings(db)))
		r.Post("/push/files", capGate(authFunc, data.CapSiteConfigure, handlePushFiles(db)))
		r.Get("/pull/data", capGate(authFunc, data.CapSiteConfigure, handlePullData(db)))
		r.Get("/pull/users", capGate(authFunc, data.CapSiteConfigure, handlePullUsers(db)))
		r.Get("/pull/settings", capGate(authFunc, data.CapSiteConfigure, handlePullSettings(db)))
		r.Get("/pull/files", capGate(authFunc, data.CapSiteConfigure, handlePullFiles(db)))
	})
}

// capGate wraps a handler, requiring an authenticated user whose role holds the
// given capability. 401 when unauthenticated, 403 when the capability is missing.
func capGate(authFunc func(*http.Request) *data.User, cap data.Capability, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !user.Can(cap) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// --- Content (collections + records) ---

// defaultCollections appear in the collections list when friendo.toml declares no
// [content] collections, so a fresh site has somewhere to create the first record.
var defaultCollections = []string{"blog", "pages", "posts"}

// handleListCollections lists the site's collections: the types friendo.toml
// declares (in that order, with their fields), then any other collection that
// has records — a collection can be started ad hoc, and the toml can catch up
// later (see handleContentToml). With no declared types, the defaults stand in.
func handleListCollections(db *data.DB, types []ContentType, declared bool, profileFields []FieldDecl) http.HandlerFunc {
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

		out := []collectionInfo{}
		seen := map[string]bool{}
		if declared {
			for _, t := range types {
				fields := t.Fields
				if fields == nil {
					fields = []FieldDecl{}
				}
				out = append(out, collectionInfo{Name: t.Name, Count: byName[t.Name], Declared: true, Fields: fields})
				seen[t.Name] = true
			}
		} else {
			for _, name := range defaultCollectionsFor(db) {
				out = append(out, collectionInfo{Name: name, Count: byName[name], Fields: []FieldDecl{}})
				seen[name] = true
			}
		}
		for _, c := range counts {
			if !seen[c.Name] {
				out = append(out, collectionInfo{Name: c.Name, Count: c.Count, Fields: []FieldDecl{}})
			}
		}
		jsonResponse(w, map[string]any{"collections": out, "declared": declared, "profile_fields": profileFields})
	}
}

// handleListRecords lists a collection's records. Editors+ (content.edit.any)
// see every record; contributors see only their own.
func handleListRecords(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		collection := chi.URLParam(r, "collection")
		user := authFunc(r)
		var records []map[string]any
		var err error
		if user.Can(data.CapContentEditAny) {
			records, err = db.QueryCollection(collection)
		} else {
			records, err = db.QueryCollectionOwnedBy(collection, user.ID)
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		if records == nil {
			records = []map[string]any{}
		}
		attachWhenAll(db, records)
		jsonResponse(w, map[string]any{"posts": records})
	}
}

// attachWhen adds the post's calendar series as record.when (nil when the post
// has no time), the same shape templates see.
func attachWhen(db *data.DB, record map[string]any) {
	id, _ := record["id"].(string)
	if id == "" {
		return
	}
	if ev := db.EventFor(id); ev != nil {
		record["when"] = ev.JSON(time.Now())
	} else {
		record["when"] = nil
	}
}

// attachWhenAll does attachWhen for a list with one query.
func attachWhenAll(db *data.DB, records []map[string]any) {
	db.AttachWhen(records, time.Now(), true)
}

func handleGetRecord(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		record, err := db.GetRecordByID(id)
		if err == sql.ErrNoRows {
			jsonError(w, "post not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		user := authFunc(r)
		if !user.Can(data.CapContentEditAny) && !db.UserOwnsPost(user.ID, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		attachWhen(db, record)
		jsonResponse(w, map[string]any{"post": record})
	}
}

// validRecordStatus is the set of publication states a post may hold.
func validRecordStatus(s string) bool {
	switch s {
	case "draft", "pending", "published":
		return true
	}
	return false
}

// handleListRecordsByStatus returns posts across collections with ?status
// (default pending) — the review queue for editors.
func handleListRecordsByStatus(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		records, err := db.ListRecordsByStatus(status)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		attachWhenAll(db, records)
		jsonResponse(w, map[string]any{"posts": records})
	}
}

// handleSetRecordStatus flips a post's publication status without touching its
// content. A moderator (review.posts) may only answer the review queue: a
// pending post becomes published (approved) or draft (rejected). An editor
// (content.publish) may set any status.
func handleSetRecordStatus(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if !validRecordStatus(in.Status) {
			jsonError(w, "invalid status", http.StatusBadRequest)
			return
		}
		id := chi.URLParam(r, "id")
		if user := authFunc(r); !user.Can(data.CapContentPublish) {
			current, err := db.GetRecordByID(id)
			if err == sql.ErrNoRows {
				jsonError(w, "post not found", http.StatusNotFound)
				return
			}
			if err != nil {
				jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
				return
			}
			if current["status"] != "pending" || in.Status == "pending" {
				jsonError(w, "a moderator can only approve or reject a post waiting for review", http.StatusForbidden)
				return
			}
		}
		err := db.SetRecordStatus(id, in.Status)
		if err == sql.ErrNoRows {
			jsonError(w, "post not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		record, _ := db.GetRecordByID(id)
		jsonResponse(w, map[string]any{"post": record})
	}
}

// recordStatusFor decides a new/edited post's status. Authors who can publish
// keep the status they requested; others get 'pending' when the site requires
// approval, otherwise 'published'.
func recordStatusFor(db *data.DB, user *data.User, requested string) string {
	if user.Can(data.CapContentPublish) {
		return requested
	}
	if db.GetBoolSetting(settingPostsNeedReview, false) {
		return "pending"
	}
	return "published"
}

type recordInput struct {
	Slug   string          `json:"slug"`
	Title  string          `json:"title"`
	Body   string          `json:"body"`
	Status string          `json:"status"`
	Data   json.RawMessage `json:"fields"` // optional front-matter fields; omitted leaves data untouched
}

func (in recordInput) status() string {
	if in.Status == "" {
		return "draft"
	}
	return in.Status
}

func handleCreateRecord(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		collection := chi.URLParam(r, "collection")
		if collection == "profiles" {
			// /profiles/<slug> pages read the authors table, not posts.
			jsonError(w, "\"profiles\" is reserved for member profiles", http.StatusBadRequest)
			return
		}
		// Contributors+ (content.create) create directly. A member without that
		// capability may submit only when the site opts in; their post is
		// rate-limited and forced into the pending review queue. A group is the
		// exception: with member_groups on, a member makes one live at once and
		// becomes its first moderator.
		memberSubmission, memberGroup := false, false
		if !user.Can(data.CapContentCreate) {
			switch {
			case collection == data.GroupsCollection && db.FeatureOn("groups") && db.MembersCanStartGroups():
				if !db.RateLimitAllow("group:"+user.ID, commentRateLimit, commentRateWindow) {
					jsonError(w, "you're creating groups too fast — slow down", http.StatusTooManyRequests)
					return
				}
				memberGroup = true
			case db.GetBoolSetting(settingMembersCanPost, false):
				if !db.RateLimitAllow("submission:"+user.ID, commentRateLimit, commentRateWindow) {
					jsonError(w, "you're submitting too fast — slow down", http.StatusTooManyRequests)
					return
				}
				memberSubmission = true
			default:
				jsonError(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		var in recordInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		authorID := db.DefaultAuthorID(user.ID)
		status := recordStatusFor(db, user, in.status())
		if memberSubmission {
			status = "pending" // community submissions always enter the review queue
		}
		if memberGroup {
			status = "published"
		}
		id, err := db.CreateRecord(collection, in.Slug, in.Title, in.Body, status, authorID)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		if len(in.Data) > 0 && string(in.Data) != "null" {
			cleaned := liftWhen(db, id, in.Data)
			db.SetRecordData(id, string(cleaned))
			autoGeotag(db, id, in.Title, cleaned)
		}
		// Whoever makes a group is its first admin.
		if collection == data.GroupsCollection && authorID != "" {
			db.SetMembership(id, authorID, data.GroupRoleAdmin, data.MembershipMember)
		}
		record, _ := db.GetRecordByID(id)
		attachWhen(db, record)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"post": record})
	}
}

// administersGroupRecord: a group's admins may edit its record (title, body,
// settings) even when they didn't found it — the capability gate on the route
// still needs content.create, so this reaches contributors who run a group.
func administersGroupRecord(db *data.DB, user *data.User, record map[string]any) bool {
	if c, _ := record["collection"].(string); c != data.GroupsCollection {
		return false
	}
	return db.IsGroupAdmin(record["id"].(string), db.DefaultAuthorID(user.ID))
}

// reviewsPendingRecord: a moderator (review.posts) may throw out a post that is
// waiting for review, the same as rejecting it.
func reviewsPendingRecord(db *data.DB, user *data.User, id string) bool {
	if !user.Can(data.CapReviewPosts) {
		return false
	}
	rec, err := db.GetRecordByID(id)
	return err == nil && rec["status"] == "pending"
}

// liftWhen pulls `when` (a string, or {start, end, timezone, repeats, except,
// rrule}) out of a record's fields into the post's events row and returns the
// fields without it — the API-side twin of the content importer's lifting, so a
// <friendo-form> with an <input name="when"> makes an event with no SDK knowledge.
//
// Data with no `when` key leaves the post's event as it is: the API hands `data`
// back without the lifted keys, so a client that reads a record and writes it
// back (the form does, after a photo upload) must not erase the time. Removing
// the event is explicit — `"when": null` or `"when": ""`. A `when` that can't
// be read is logged and the existing event kept; the record still saves.
func liftWhen(db *data.DB, postID string, raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return raw
	}
	when, present := m["when"]
	ev, warnings := data.LiftWhen(m, db.Location)
	for _, w := range warnings {
		log.Printf("record %s: %s", postID, w)
	}
	switch {
	case !present:
		// Nothing said about the time; keep what's there.
	case ev == nil && (when == nil || when == ""):
		if err := db.ReconcileWhen(postID, nil); err != nil {
			log.Printf("record %s when: %v", postID, err)
		}
	case ev == nil:
		// Present but unreadable (warned above): keep the existing event.
	default:
		if err := db.ReconcileWhen(postID, ev); err != nil {
			log.Printf("record %s when: %v", postID, err)
		}
	}
	cleaned, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return cleaned
}

// autoGeotag scans a newly created record's data blob for fields shaped like
// {lat, lng} and geo-tags the post with each. A post authored through
// <friendo-form>'s location input stores its pick in data.<name>, so this makes it
// appear on <friendo-map> with no extra call — and, being server-side, it works for
// member submissions too (they can't reach the contributor-gated locations API).
// Out-of-range coordinates are skipped. Runs on create only (the form is create-only).
func autoGeotag(db *data.DB, postID, title string, raw json.RawMessage) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return
	}
	// Deterministic order so multiple pins tag in a stable sequence.
	names := make([]string, 0, len(data))
	for k := range data {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		obj, ok := data[k].(map[string]any)
		if !ok {
			continue
		}
		lat, latOK := obj["lat"].(float64)
		lng, lngOK := obj["lng"].(float64)
		if !latOK || !lngOK || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			continue
		}
		db.CreateLocation("post", postID, lat, lng, title)
	}
}

func handleUpdateRecord(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		current, err := db.GetRecordByID(id)
		if err == sql.ErrNoRows {
			jsonError(w, "post not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		user := authFunc(r)
		if !user.Can(data.CapContentEditAny) && !db.UserOwnsPost(user.ID, id) && !administersGroupRecord(db, user, current) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}

		var in recordInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Authors who can't publish cannot change a post's publication state.
		status := in.status()
		if !user.Can(data.CapContentPublish) {
			status, _ = current["status"].(string)
		}
		if err := db.UpdateRecord(id, in.Slug, in.Title, in.Body, status); err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		if len(in.Data) > 0 && string(in.Data) != "null" {
			db.SetRecordData(id, string(liftWhen(db, id, in.Data)))
		}
		record, _ := db.GetRecordByID(id)
		attachWhen(db, record)
		jsonResponse(w, map[string]any{"post": record})
	}
}

func handleDeleteRecord(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		user := authFunc(r)
		if !user.Can(data.CapContentEditAny) && !db.UserOwnsPost(user.ID, id) && !reviewsPendingRecord(db, user, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		err := db.DeleteRecord(id)
		if err == sql.ErrNoRows {
			jsonError(w, "post not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		db.DeleteMembershipsForGroup(id) // a no-op for anything but a group
		db.DeleteChatsForGroup(id)
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

// handleListComments returns a post's comments for the current viewer. Anonymous
// visitors see approved comments only. A signed-in member also sees their own
// pending comment; the post's author or a full moderator sees everything and gets
// `can_moderate: true` so the SDK can show inline moderation controls.
func handleListComments(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		postID := chi.URLParam(r, "id")
		viewerID := ""
		canModerate := false
		if u := authFunc(r); u != nil {
			viewerID = u.ID
			canModerate = u.Can(data.CapReviewAny) || db.UserOwnsPost(u.ID, postID)
		}
		comments, err := db.ListCommentsForViewer(postID, viewerID, canModerate)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"comments": comments, "can_moderate": canModerate})
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
		if !db.RateLimitAllow("comment:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "you're commenting too fast — slow down", http.StatusTooManyRequests)
			return
		}
		status := "pending"
		if !commentsNeedReview(db) {
			status = "approved"
		}
		authorID := db.DefaultAuthorID(user.ID)
		id, err := db.CreateComment(chi.URLParam(r, "id"), in.ParentID, authorID, in.Body, status)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		if status == "approved" {
			notifyCommentOnPost(db, id)
		}
		comment, _ := db.GetComment(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"comment": comment})
	}
}

// handleModerationList returns comments filtered by ?status (defaults to the
// pending queue). Moderators+ (review.any) see the whole site; a
// Contributor sees only comments on posts they authored.
func handleModerationList(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		user := authFunc(r)
		var comments []map[string]any
		var err error
		if user.Can(data.CapReviewAny) {
			comments, err = db.ListCommentsByStatus(status)
		} else {
			comments, err = db.ListCommentsByStatusForOwner(status, user.ID)
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"comments": comments})
	}
}

// canModerateComment reports whether the actor may moderate this comment — any
// comment with moderate.any, else only comments on their own posts.
func canModerateComment(db *data.DB, user *data.User, commentID string) bool {
	return user.Can(data.CapReviewAny) || db.UserOwnsCommentPost(user.ID, commentID)
}

// handleUpdateComment changes a comment's moderation status.
func handleUpdateComment(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !canModerateComment(db, authFunc(r), id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
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
		err := db.UpdateCommentStatus(id, in.Status)
		if err == sql.ErrNoRows {
			jsonError(w, "comment not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		if in.Status == "approved" {
			notifyCommentOnPost(db, id)
		}
		comment, _ := db.GetComment(id)
		jsonResponse(w, map[string]any{"comment": comment})
	}
}

// handleDeleteComment removes a comment. Allowed for the comment's author (a
// member deleting their own), or anyone who can moderate it.
func handleDeleteComment(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := chi.URLParam(r, "id")
		if !db.UserOwnsComment(user.ID, id) && !canModerateComment(db, user, id) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		err := db.DeleteComment(id)
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

// targetOf reads what a reaction or location points at: post_id names a post,
// comment_id a comment (reactions only). Returns "", "" when neither is given.
func targetOf(get func(string) string) (targetType, targetID string) {
	if id := get("post_id"); id != "" {
		return "post", id
	}
	if id := get("comment_id"); id != "" {
		return "comment", id
	}
	return "", ""
}

// handleListReactions returns per-emoji counts for a post or comment (public).
// If the caller is an authenticated member, each entry reports whether they reacted.
func handleListReactions(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType, targetID := targetOf(r.URL.Query().Get)
		if targetID == "" {
			jsonError(w, "post_id or comment_id is required", http.StatusBadRequest)
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
			PostID    string `json:"post_id"`
			CommentID string `json:"comment_id"`
			Emoji     string `json:"emoji"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		targetType, targetID := targetOf(func(k string) string {
			if k == "post_id" {
				return in.PostID
			}
			return in.CommentID
		})
		if targetID == "" || in.Emoji == "" {
			jsonError(w, "post_id (or comment_id) and emoji are required", http.StatusBadRequest)
			return
		}
		authorID := db.DefaultAuthorID(user.ID)
		reacted, err := db.ToggleReaction(targetType, targetID, authorID, in.Emoji)
		if err != nil {
			jsonError(w, fmt.Sprintf("reaction error: %v", err), http.StatusInternalServerError)
			return
		}
		reactions, _ := db.ReactionCounts(targetType, targetID, authorID)
		jsonResponse(w, map[string]any{"reacted": reacted, "reactions": reactions})
	}
}

// --- RSVP ---

// rsvpPayload is the shape every RSVP call answers with: the date asked about,
// its tally, the caller's own answer, and (for organizers) who answered.
func rsvpPayload(db *data.DB, user *data.User, ev *data.Event, key string) map[string]any {
	counts, _ := db.RSVPCountsFor(ev.ID, key)
	out := map[string]any{
		"date":   key,
		"counts": counts,
		"mine":   "",
	}
	if t, err := time.Parse(time.RFC3339, key); err == nil {
		out["date_text"] = data.FormatWhen(t.In(ev.Location()), time.Time{}, ev.AllDay, time.Now().In(ev.Location()), "")
	}
	if user != nil {
		out["mine"] = db.RSVPForUser(ev.ID, key, user.ID)
		if user.Can(data.CapReviewAny) || db.UserOwnsEventPost(user.ID, ev) {
			if list, err := db.ListRSVPs(ev.ID, key); err == nil {
				out["names"] = list
			}
		}
	}
	return out
}

// eventForPost resolves a post's series and the date a request names
// (?date= or the body's; "" = the next one), or writes the error.
func eventForPost(w http.ResponseWriter, db *data.DB, postID, requested string) (*data.Event, string, bool) {
	rec, err := db.GetRecordByID(postID)
	if err != nil {
		jsonError(w, "post not found", http.StatusNotFound)
		return nil, "", false
	}
	if st, _ := rec["status"].(string); st != "published" {
		jsonError(w, "post not found", http.StatusNotFound)
		return nil, "", false
	}
	ev := db.EventFor(postID)
	if ev == nil {
		jsonError(w, "this post has no time to RSVP to", http.StatusNotFound)
		return nil, "", false
	}
	key, err := ev.ResolveOccurrence(strings.TrimSpace(requested), time.Now())
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return nil, "", false
	}
	return ev, key, true
}

// handleGetRSVPs returns the tally for a date (public).
func handleGetRSVPs(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ev, key, ok := eventForPost(w, db, chi.URLParam(r, "id"), r.URL.Query().Get("date"))
		if !ok {
			return
		}
		jsonResponse(w, rsvpPayload(db, authFunc(r), ev, key))
	}
}

// handleSetRSVP records the caller's answer for a date (any signed-in member,
// or a visitor with a name when visitors_can_rsvp is on).
func handleSetRSVP(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "sign in to RSVP", http.StatusUnauthorized)
			return
		}
		var in struct {
			Occurrence string `json:"date"`
			Answer     string `json:"answer"`
			// A visitor's name: an organizer needs to tell who's coming, so a
			// visitor gives one the first time they answer.
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if !data.ValidRSVPAnswer(in.Answer) {
			jsonError(w, "answer must be going, not_going, or maybe", http.StatusBadRequest)
			return
		}
		ev, key, ok := eventForPost(w, db, chi.URLParam(r, "id"), in.Occurrence)
		if !ok {
			return
		}
		if user.IsVisitor() {
			if strings.TrimSpace(in.Name) != "" {
				if err := db.SetVisitorName(user.ID, in.Name); err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "needs_name": true})
					return
				}
			} else if db.VisitorName(user.ID) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{"error": "add your name so the organizer knows who's coming", "needs_name": true})
				return
			}
		}
		if !db.RateLimitAllow("rsvp:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "too many changes — slow down", http.StatusTooManyRequests)
			return
		}
		if err := db.SetRSVP(ev.ID, key, db.DefaultAuthorID(user.ID), in.Answer); err != nil {
			jsonError(w, fmt.Sprintf("rsvp error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, rsvpPayload(db, user, ev, key))
	}
}

// handleDeleteRSVP withdraws the caller's answer for an occurrence.
func handleDeleteRSVP(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ev, key, ok := eventForPost(w, db, chi.URLParam(r, "id"), r.URL.Query().Get("date"))
		if !ok {
			return
		}
		// Any of the account's profiles may have answered; clear them all.
		profiles, _ := db.ListProfilesFor(user.ID)
		for _, p := range profiles {
			if id, _ := p["id"].(string); id != "" {
				db.DeleteRSVP(ev.ID, key, id)
			}
		}
		jsonResponse(w, rsvpPayload(db, user, ev, key))
	}
}

// handleRSVPNames lists every answer on a post's event for its organizer (the
// post's author, or a moderator), grouped by date; ?format=csv downloads it.
func handleRSVPNames(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		postID := chi.URLParam(r, "id")
		ev := db.EventFor(postID)
		if ev == nil {
			jsonError(w, "this post has no time to RSVP to", http.StatusNotFound)
			return
		}
		if !user.Can(data.CapReviewAny) && !db.UserOwnsEventPost(user.ID, ev) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		rows, err := db.AttendeeRows(ev)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("format") == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="rsvps.csv"`)
			cw := csv.NewWriter(w)
			cw.Write([]string{"date", "name", "email", "answer", "answered_at", "scheduled"})
			for _, row := range rows {
				str := func(k string) string { v, _ := row[k].(string); return v }
				sched := "yes"
				if s, _ := row["scheduled"].(bool); !s {
					sched = "no longer scheduled"
				}
				cw.Write([]string{str("date"), str("author_name"), str("author_email"), str("answer"), str("updated"), sched})
			}
			cw.Flush()
			return
		}
		jsonResponse(w, map[string]any{"names": rows})
	}
}

// --- Locations (geo-tagging) ---

// handleListLocations returns locations (public). With post_id it returns one
// post's locations. Without it, every published post's location — the aggregate
// "all posts on one map" query — optionally bounded by a
// bbox=minLng,minLat,maxLng,maxLat viewport, each row decorated with the post's
// public "url" (via the permalink resolver) so markers can link back. The
// aggregate path is published-only so it can't leak draft positions.
func handleListLocations(db *data.DB, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetType, targetID := "post", r.URL.Query().Get("post_id")
		if targetID != "" {
			locations, err := db.ListLocations(targetType, targetID)
			if err != nil {
				jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, map[string]any{"locations": locations})
			return
		}
		locations, err := db.ListLocationsByType(targetType, parseBBox(r.URL.Query().Get("bbox")))
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		if permalink != nil {
			for _, loc := range locations {
				collection, _ := loc["collection"].(string)
				slug, _ := loc["slug"].(string)
				if collection != "" && slug != "" {
					loc["url"] = permalink(collection, map[string]string{"slug": slug})
				}
			}
		}
		jsonResponse(w, map[string]any{"locations": locations})
	}
}

// parseBBox parses a "minLng,minLat,maxLng,maxLat" query value into a *data.BBox,
// or nil if empty/malformed (treated as "no bound"). The order follows GeoJSON /
// Leaflet's toBBoxString (west,south,east,north).
func parseBBox(s string) *data.BBox {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return nil
	}
	v := make([]float64, 4)
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil
		}
		v[i] = f
	}
	return &data.BBox{MinLng: v[0], MinLat: v[1], MaxLng: v[2], MaxLat: v[3]}
}

// handleCreateLocation gives a post a location. Contributors may place a post
// they authored; editors+ (content.edit.any) may place anything.
func handleCreateLocation(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PostID string  `json:"post_id"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			Label  string  `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		targetType, targetID := "post", in.PostID
		if targetID == "" {
			jsonError(w, "post_id is required", http.StatusBadRequest)
			return
		}
		if in.Lat < -90 || in.Lat > 90 || in.Lng < -180 || in.Lng > 180 {
			jsonError(w, "lat/lng out of range", http.StatusBadRequest)
			return
		}
		if !canTagTarget(db, authFunc(r), targetType, targetID) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := db.CreateLocation(targetType, targetID, in.Lat, in.Lng, in.Label)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		location, _ := db.GetLocation(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"location": location})
	}
}

// handleDeleteLocation removes a location. Same ownership rule as create: the
// actor must own the location's target post, or hold content.edit.any.
func handleDeleteLocation(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		loc, err := db.GetLocation(id)
		if err == sql.ErrNoRows {
			jsonError(w, "location not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		tt, _ := loc["target_type"].(string)
		tid, _ := loc["target_id"].(string)
		if !canTagTarget(db, authFunc(r), tt, tid) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := db.DeleteLocation(id); err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// canTagTarget reports whether the user may attach/remove a location on a target.
// Editors+ (content.edit.any) may tag anything; a contributor may tag only a post
// they authored. Non-post targets have no ownership model, so they need edit.any.
func canTagTarget(db *data.DB, user *data.User, targetType, targetID string) bool {
	if user == nil {
		return false
	}
	if user.Can(data.CapContentEditAny) {
		return true
	}
	return targetType == "post" && db.UserOwnsPost(user.ID, targetID)
}

// --- Files (per-record media) ---

// extForMime maps an image content-type to a file extension, so uploads keep a
// sensible extension even when the client omits a filename.
func extForMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}

// handleListFiles returns the images attached to a post (public — URLs are public).
func handleListFiles(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recordType, recordID := "post", r.URL.Query().Get("post_id")
		if recordID == "" {
			jsonError(w, "post_id is required", http.StatusBadRequest)
			return
		}
		files, err := db.ListFiles(recordType, recordID)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"files": files})
	}
}

// handleUploadFile accepts a multipart image upload and links it to a record
// (contributor+). The bytes land in the site's assets/ dir so the existing
// /assets/* static handler serves them; the edge runtime stores the same key in R2.
func handleUploadFile(db *data.DB, siteDir string, store storage.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			jsonError(w, "expected a multipart form upload", http.StatusBadRequest)
			return
		}
		recordType, recordID := "post", r.FormValue("post_id")
		if recordID == "" {
			jsonError(w, "post_id is required", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, "a file field is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		mime := header.Header.Get("Content-Type")
		if !strings.HasPrefix(mime, "image/") {
			jsonError(w, "only image uploads are supported", http.StatusBadRequest)
			return
		}
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext == "" {
			ext = extForMime(mime)
		}
		id := data.GenerateID()
		key := "assets/uploads/" + id + ext

		size, err := writeAsset(r.Context(), siteDir, store, key, io.LimitReader(file, 10<<20), mime)
		if err != nil {
			jsonError(w, fmt.Sprintf("storage error: %v", err), http.StatusInternalServerError)
			return
		}

		if _, err := db.CreateFileWithID(id, recordType, recordID, r.FormValue("field"), key, mime, size); err != nil {
			removeAsset(r.Context(), siteDir, store, key)
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		out, _ := db.GetFile(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"file": out})
	}
}

// handleDeleteFile removes a file row and the stored object (contributor+).
func handleDeleteFile(db *data.DB, siteDir string, store storage.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, err := db.DeleteFile(chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "file not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		// Best-effort removal of the stored object; the DB row is the source of truth.
		removeAsset(r.Context(), siteDir, store, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeAsset stores an uploaded asset on local disk (store == nil) or in the media
// backend, returning the byte count written.
func writeAsset(ctx context.Context, siteDir string, store storage.Backend, key string, r io.Reader, contentType string) (int64, error) {
	if store != nil {
		buf, err := io.ReadAll(r)
		if err != nil {
			return 0, err
		}
		if err := store.Put(ctx, key, bytes.NewReader(buf), int64(len(buf)), contentType); err != nil {
			return 0, err
		}
		return int64(len(buf)), nil
	}
	fullPath := filepath.Join(siteDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return 0, err
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(dst, r)
	dst.Close()
	if err != nil {
		os.Remove(fullPath)
		return 0, err
	}
	return size, nil
}

// removeAsset deletes a stored asset from disk or the media backend (best-effort).
func removeAsset(ctx context.Context, siteDir string, store storage.Backend, key string) {
	if store != nil {
		store.Delete(ctx, key)
		return
	}
	os.Remove(filepath.Join(siteDir, filepath.FromSlash(key)))
}

// --- Chats & messages (community feed) ---

// messageHub fans out new messages to open SSE streams, per chat. The client
// is <friendo-chat>, an EventSource on /chats/:id/stream.
type messageHub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{} // chatID -> subscribers
}

var msgHub = &messageHub{subs: map[string]map[chan string]struct{}{}}

func (h *messageHub) subscribe(chatID string) chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	if h.subs[chatID] == nil {
		h.subs[chatID] = map[chan string]struct{}{}
	}
	h.subs[chatID][ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *messageHub) unsubscribe(chatID string, ch chan string) {
	h.mu.Lock()
	if set := h.subs[chatID]; set != nil {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subs, chatID)
		}
	}
	h.mu.Unlock()
	close(ch)
}

// broadcast sends one SSE frame to everyone streaming the chat. An empty event
// name is the default `message` event (a new message); a named one — `delete`
// — carries a change to what's already shown.
func (h *messageHub) broadcast(chatID, event, data string) {
	frame := "data: " + data + "\n\n"
	if event != "" {
		frame = "event: " + event + "\n" + frame
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[chatID] {
		select {
		case ch <- frame:
		default: // drop for a slow/full subscriber rather than block the poster
		}
	}
}

// handleChatStream is a Server-Sent Events stream of what changes in a chat
// (public): each default frame is a new message in the same shape as the list
// API; an `event: delete` frame is `{"id": …}` for a message that's gone.
func handleChatStream(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chatID := chi.URLParam(r, "id")
		chat, err := db.GetChat(chatID)
		if err != nil {
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		if g, _ := chat["group_id"].(string); g != "" {
			// A group's chat streams under its group (with the membership check).
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		streamChat(w, r, chatID)
	}
}

// streamChat is the SSE body shared by site and group chats.
func streamChat(w http.ResponseWriter, r *http.Request, chatID string) {
	{
		flusher, ok := w.(http.Flusher)
		if !ok {
			jsonError(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := msgHub.subscribe(chatID)
		defer msgHub.unsubscribe(chatID, ch)
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		for {
			select {
			case frame := <-ch:
				fmt.Fprint(w, frame)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

func handleListChats(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chats, err := db.ListChats()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"chats": chats})
	}
}

// handleCreateChat creates a chat by hand (admin-gated). Pages make their own
// chats just by naming one (<friendo-chat chat-id="general"> registers `general`
// the first time the page is served), so this is for making one ahead of time
// or with a display name. `id` is optional (a generated one otherwise) and must
// be a slug; `name` defaults to the id.
func handleCreateChat(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		in.ID, in.Name = strings.TrimSpace(in.ID), strings.TrimSpace(in.Name)
		if in.ID == "" && in.Name == "" {
			jsonError(w, "an id or a name is required", http.StatusBadRequest)
			return
		}
		if in.ID != "" {
			if !data.ValidChatID(in.ID) {
				jsonError(w, "id must be letters, digits, dots, dashes or underscores (up to 64)", http.StatusBadRequest)
				return
			}
			if _, err := db.GetChat(in.ID); err == nil {
				jsonError(w, "a chat with that id already exists", http.StatusConflict)
				return
			}
		}
		id, err := db.CreateChat(in.ID, in.Name, in.Kind)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		chat, _ := db.GetChat(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"chat": chat})
	}
}

// handleDeleteChat removes a chat and its messages (admin-gated).
func handleDeleteChat(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := db.DeleteChat(chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleListMessages returns a chat's messages (public; `mine` per row when
// the caller is signed in).
func handleListMessages(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		chatID := chi.URLParam(r, "id")
		chat, err := db.GetChat(chatID)
		if err != nil {
			chat = map[string]any{"group_id": ""} // a page names it before the tag asks; unknown = empty
		} else if g, _ := chat["group_id"].(string); g != "" {
			jsonError(w, "chat not found", http.StatusNotFound) // a group's chat lives under its group
			return
		}
		serveMessages(db, w, user, chatID, chatAccessFor(db, user, chat))
	}
}

// serveMessages answers a chat's messages with what the viewer may do there
// (`can_moderate`, so the tag shows delete on the right bubbles).
func serveMessages(db *data.DB, w http.ResponseWriter, user *data.User, chatID string, access chatAccess) {
	viewerID := ""
	if user != nil {
		viewerID = user.ID
	}
	messages, err := db.ListMessages(chatID, viewerID)
	if err != nil {
		jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{"messages": messages, "can_moderate": access.canModerate, "can_post": access.canPost})
}

// handlePostMessage posts a message to a chat. Member-gated + rate-limited.
func handlePostMessage(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		chatID := chi.URLParam(r, "id")
		chat, err := db.GetChat(chatID)
		if err != nil {
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		if g, _ := chat["group_id"].(string); g != "" {
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		postMessage(db, w, r, user, chatID)
	}
}

// postMessage is the body shared by site and group chats: validate, rate-limit,
// write, and push to everyone streaming.
func postMessage(db *data.DB, w http.ResponseWriter, r *http.Request, user *data.User, chatID string) {
	{
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
		if !db.RateLimitAllow("message:"+user.ID, commentRateLimit, commentRateWindow) {
			jsonError(w, "you're posting too fast — slow down", http.StatusTooManyRequests)
			return
		}
		id, err := db.CreateMessage(chatID, in.ParentID, db.DefaultAuthorID(user.ID), in.Body)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		message, _ := db.GetMessage(id)
		// Push the new message to everyone streaming this chat.
		if b, err := json.Marshal(message); err == nil {
			msgHub.broadcast(chatID, "", string(b))
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"message": message})
	}
}

// handleDeleteMessage removes a message — its author, or whoever moderates its
// chat (comment moderators; a group's moderators for the group's chats).
func handleDeleteMessage(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := chi.URLParam(r, "id")
		message, err := db.GetMessage(id) // for its chat: the rules, and the stream
		if err == sql.ErrNoRows {
			jsonError(w, "message not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		chat, _ := db.GetChat(message["chat_id"].(string))
		if chat == nil {
			chat = map[string]any{"group_id": ""}
		}
		access := chatAccessFor(db, user, chat)
		if !access.canRead || (!db.UserOwnsMessage(user.ID, id) && !access.canModerate) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		err = db.DeleteMessage(id)
		if err == sql.ErrNoRows {
			jsonError(w, "message not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		// Take it off everyone's screen, not just the deleter's.
		if chatID, _ := message["chat_id"].(string); chatID != "" {
			if b, err := json.Marshal(map[string]string{"id": id}); err == nil {
				msgHub.broadcast(chatID, "delete", string(b))
			}
		}
		w.WriteHeader(http.StatusNoContent)
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

// The cookie is site-wide (Path /) so a page render can see who's viewing — that's
// what lets a template say {{ user.name }} or {% members only %}. Before members-only
// pages it was scoped to /_/ (admin + API only). SameSite is Lax rather than Strict
// so a top-level link from an email or a chat to a members-only page still carries
// the session; every mutating API call is a JSON POST/PUT/DELETE from fetch, which
// Lax never attaches cross-site, so the CSRF posture is unchanged.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	setCookieFor(w, r, token, 7*24*time.Hour)
}

// setCookieFor issues the session cookie with a lifetime to match its session
// (a visitor's lasts much longer than a member's; see visitors.go).
func setCookieFor(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		// Secure only over HTTPS — a Secure cookie isn't sent over plain http,
		// which would break local `friendo serve` on http://localhost.
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

// SetSessionCookie issues the site session cookie with the runtime's standard
// attributes. Exported for the network's browser "Open admin" landing, which
// mints a site session out of process from this package's handlers.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	setSessionCookie(w, r, token)
}

// requestIsHTTPS reports whether the request arrived over TLS (directly or via a
// terminating proxy).
func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// clearSessionCookie expires the session cookie on both its current path (/) and
// the legacy /_/ path. Browsers key cookies by name + domain + path, so a browser
// that signed in before the cookie went site-wide still holds a /_/ cookie — and
// would stay signed in to the admin after logout until it expired.
func clearSessionCookie(w http.ResponseWriter) {
	for _, p := range []string{"/", "/_/"} {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     p,
			HttpOnly: true,
			MaxAge:   -1,
		})
	}
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

// handleMe returns the current user, or 401 if not authenticated. Beside the
// account it names the profile attribution flows to (author_id), so a tag can
// tell "this is me" without a second call.
func handleMe(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		me := userJSON(user)
		authorID := db.DefaultAuthorID(user.ID)
		me["profile_id"] = authorID
		// Who they follow (author ids), so a page of follow buttons paints its
		// pressed state without a call per button. Empty while follows is off.
		me["following"] = []string{}
		if db.FeatureOn("follows") {
			me["following"] = db.Following(authorID)
		}
		// The groups they belong to, by slug.
		me["groups"] = []string{}
		if db.FeatureOn("groups") {
			me["groups"] = db.GroupsFor(authorID)
		}
		// Unread notifications, for the badge in <friendo-auth>.
		me["unread"] = 0
		if db.NotificationsOn() {
			me["unread"] = db.UnreadCount(user.ID)
		}
		jsonResponse(w, map[string]any{"user": me})
	}
}

// handleListProfilesFor returns the caller's author profiles (profiles).
func handleListProfilesFor(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		profiles, err := db.ListProfilesFor(user.ID)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"profiles": profiles})
	}
}

// handleCreateProfile adds a new profile to the caller's account.
func handleCreateProfile(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			Name   string `json:"name"`
			Avatar string `json:"avatar"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			jsonError(w, "name is required", http.StatusBadRequest)
			return
		}
		profile, err := db.CreateProfile(user.ID, in.Name, in.Avatar)
		if err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"profile": profile})
	}
}

// handleSetDefaultProfile points the caller's default at one of their profiles.
func handleSetDefaultProfile(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		err := db.SetDefaultProfile(user.ID, chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "profile not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"ok": true})
	}
}

// Rate-limit thresholds.
const (
	loginFailLimit    = 5
	loginFailWindow   = 15 * time.Minute
	commentRateLimit  = 20
	commentRateWindow = 5 * time.Minute
)

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

		email := data.NormalizeEmail(body.Email)
		// Passwords are opt-in: a site signs in with an emailed code unless its
		// owner turned passwords on (or set one up with a password to begin with).
		if !db.GetBoolSetting(settingPasswordLogin, false) {
			jsonError(w, "password sign-in is turned off for this site — sign in with a code sent to your email instead", http.StatusForbidden)
			return
		}
		// Brute-force guard: lock out after too many failed attempts per account.
		bucket := "login-fail:" + email
		if db.RateLimitExceeded(bucket, loginFailLimit, loginFailWindow) {
			jsonError(w, "too many failed attempts — try again later", http.StatusTooManyRequests)
			return
		}

		user, err := db.AuthenticateUser(email, body.Password)
		if err != nil {
			db.RateLimitHit(bucket, loginFailWindow)
			jsonError(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		db.RateLimitClear(bucket)
		carryVisitor(r, db, user)

		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			jsonError(w, "could not create session", http.StatusInternalServerError)
			return
		}

		setSessionCookie(w, r, token)
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
func handlePushTemplates(siteDir string, onTemplatesChanged func()) http.HandlerFunc {
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
		if onTemplatesChanged != nil {
			onTemplatesChanged()
		}

		jsonResponse(w, map[string]int{"written": written})
	}
}

// handlePushAssets accepts static asset files and writes them to assets/.
// Each file's content is a plain string, or base64 when "encoding":"base64" —
// which is how binary assets (images uploaded via the media API) round-trip
// intact, since JSON strings can't carry raw bytes.
// Body: {"files": [{"path": "assets/logo.png", "content": "...", "encoding": "base64"}]}
func handlePushAssets(siteDir string, store storage.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Files []struct {
				Path     string `json:"path"`
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
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

			content := []byte(f.Content)
			if f.Encoding == "base64" {
				decoded, err := base64.StdEncoding.DecodeString(f.Content)
				if err != nil {
					log.Printf("Error decoding %s: %v", cleanPath, err)
					continue
				}
				content = decoded
			}

			// Managed media (uploads + galleries) goes to the media backend when
			// configured; static assets always live on disk (Pongo2 and static
			// serving read them there).
			slashPath := filepath.ToSlash(cleanPath)
			if store != nil && storage.IsManagedMedia(strings.TrimPrefix(slashPath, "assets/")) {
				if err := store.Put(r.Context(), slashPath, bytes.NewReader(content), int64(len(content)), ""); err != nil {
					log.Printf("Error storing %s: %v", cleanPath, err)
					continue
				}
				written++
				continue
			}

			fullPath := filepath.Join(siteDir, cleanPath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				log.Printf("Error creating dir for %s: %v", cleanPath, err)
				continue
			}
			if err := os.WriteFile(fullPath, content, 0o644); err != nil {
				log.Printf("Error writing %s: %v", cleanPath, err)
				continue
			}
			written++
		}

		jsonResponse(w, map[string]int{"written": written})
	}
}

// handlePushData upserts records into the local database.
// Expects JSON body: {"posts": [{"id": "...", "collection": "blog", ...}]}
func handlePushData(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Records     []map[string]any `json:"posts"`
			Events      []map[string]any `json:"events"`      // calendar series, beside their posts
			Memberships []map[string]any `json:"memberships"` // who's in which group, beside the group posts
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		for _, m := range body.Memberships {
			str := func(key string) string { v, _ := m[key].(string); return v }
			if err := db.UpsertMembership(str("id"), str("group_id"), str("author_id"), str("role"), str("status"), str("created")); err != nil {
				log.Printf("Error upserting membership %s: %v", str("id"), err)
			}
		}

		synced := 0
		for _, row := range body.Events {
			if ev := data.EventFromRow(row); ev != nil {
				if err := db.UpsertEvent(ev); err != nil {
					log.Printf("Error upserting event %s: %v", ev.ID, err)
				}
			}
		}
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

			// `fields` (arbitrary front matter) arrives as a nested object; store as JSON.
			dataJSON := "{}"
			if d, ok := rec["fields"]; ok && d != nil {
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
			Users   []map[string]any `json:"users"`
			Authors []map[string]any `json:"authors"`
			Follows []map[string]any `json:"follows"`
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
				   avatar=excluded.avatar,
				   password_hash=CASE WHEN excluded.password_hash = '' THEN users.password_hash ELSE excluded.password_hash END,
				   role=excluded.role, auth_methods=excluded.auth_methods, updated=excluded.updated`,
				id, db.SiteID, data.NormalizeEmail(str("email")), str("phone"), str("name"),
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
					if str("password_hash") == "" {
						return `["otp"]`
					}
					return `["password","otp"]`
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

		// Author profiles travel with accounts so content's author_id stays valid.
		authors := 0
		for _, a := range body.Authors {
			str := func(key string) string { v, _ := a[key].(string); return v }
			dataJSON := ""
			if d, ok := a["fields"]; ok && d != nil {
				if b, err := json.Marshal(d); err == nil {
					dataJSON = string(b)
				}
			}
			if err := db.UpsertAuthor(str("id"), str("user_id"), str("name"), str("email"), str("avatar"), str("role"), str("slug"), str("bio"), dataJSON, str("created")); err != nil {
				log.Printf("Error upserting author %s: %v", str("id"), err)
				continue
			}
			authors++
		}

		// Follows ride with the profiles they connect.
		follows := 0
		for _, f := range body.Follows {
			str := func(key string) string { v, _ := f[key].(string); return v }
			if err := db.UpsertFollow(str("id"), str("follower_id"), str("followee_id"), str("created")); err != nil {
				log.Printf("Error upserting follow %s: %v", str("id"), err)
				continue
			}
			follows++
		}

		jsonResponse(w, map[string]int{"synced": synced, "authors": authors, "follows": follows})
	}
}

// handlePushSettings upserts site settings (access policy, moderation) — config
// that should travel with the site on deploy.
func handlePushSettings(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings map[string]string `json:"settings"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		synced := 0
		for k, v := range body.Settings {
			if err := db.SetSetting(k, v); err != nil {
				log.Printf("Error saving setting %s: %v", k, err)
				continue
			}
			synced++
		}
		jsonResponse(w, map[string]int{"synced": synced})
	}
}

// handlePullSettings returns all site settings as a flat map.
func handlePullSettings(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := db.AllSettings()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"settings": settings})
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
				"fields":       dataJSON,
			})
		}

		if records == nil {
			records = []map[string]any{}
		}
		events := []map[string]any{}
		if list, err := db.ListEvents(); err == nil {
			for _, e := range list {
				events = append(events, e.Row())
			}
		}
		memberships, _ := db.ListMemberships()
		jsonResponse(w, map[string]any{"posts": records, "events": events, "memberships": memberships})
	}
}

// handlePushFiles upserts media rows into the local database. The bytes travel
// separately via push/assets; these rows record which media belongs to which
// record so a deployed site's GET /files matches the source.
func handlePushFiles(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Files []struct {
				ID         string `json:"id"`
				RecordType string `json:"record_type"`
				RecordID   string `json:"record_id"`
				Field      string `json:"field"`
				R2Key      string `json:"r2_key"`
				Mime       string `json:"mime"`
				Size       int64  `json:"size"`
				Created    string `json:"created"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		synced := 0
		for _, f := range body.Files {
			if f.ID == "" {
				continue
			}
			if err := db.UpsertFile(f.ID, f.RecordType, f.RecordID, f.Field, f.R2Key, f.Mime, f.Size, f.Created); err != nil {
				jsonError(w, fmt.Sprintf("upsert error: %v", err), http.StatusInternalServerError)
				return
			}
			synced++
		}
		jsonResponse(w, map[string]int{"synced": synced})
	}
}

// handlePullFiles returns all media rows as JSON.
func handlePullFiles(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files, err := db.AllFiles()
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"files": files})
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
		authors, _ := db.ListAuthors()
		follows, _ := db.ListFollows()
		jsonResponse(w, map[string]any{"users": result, "authors": authors, "follows": follows})
	}
}

// --- First-run setup / migrate ---

// handleSetupStatus tells a cold client (SPA, CLI) what to do: whether the site
// still needs its first owner, whether an old single-admin password is waiting
// to be upgraded, whether passwords are allowed here, and whether a real email
// provider will deliver the sign-in code (else it's echoed / logged for dev).
func handleSetupStatus(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]any{
			"needsSetup":      !db.IsSetupDone(),
			"hasLegacyAdmin":  db.HasLegacyAdmin(),
			"passwordLogin":   db.GetBoolSetting(settingPasswordLogin, false),
			"emailConfigured": emailConfigured(),
		})
	}
}

// handleSetupRequestCode emails the code that proves who the first owner is.
// Public, but only while the site has no owner. No user row exists yet — the
// code is keyed on the email itself (data.SetupOTPKey) and the owner is created
// only when it verifies. The code is also written to the server log, so a
// self-hoster with no email provider can finish setup from the terminal.
func handleSetupRequestCode(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db.IsSetupDone() {
			jsonError(w, "setup already complete", http.StatusConflict)
			return
		}
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := data.NormalizeEmail(in.Email)
		if email == "" || !strings.Contains(email, "@") {
			jsonError(w, "a valid email is required", http.StatusBadRequest)
			return
		}
		key := data.SetupOTPKey(email)
		if db.HasFreshOTP(key, otpResendWindow) {
			jsonError(w, "a code was already sent — please wait before requesting another", http.StatusTooManyRequests)
			return
		}
		code := generateOTP()
		if err := db.CreateOTP(key, code, 10*time.Minute); err != nil {
			jsonError(w, "could not issue code", http.StatusInternalServerError)
			return
		}
		sendOTPEmail(email, code)
		if !emailConfigured() {
			log.Printf("Setup code for %s: %s  (no email provider configured — enter this code to create the owner account)", email, code)
		}
		resp := map[string]any{"sent": true, "emailed": emailConfigured()}
		if otpEchoEnabled() {
			resp["code"] = code
		}
		jsonResponse(w, resp)
	}
}

// handleSetupCreate creates the site's first owner. Two ways in:
//   - {email, name, code}     — the code from /setup/request-code (the default).
//   - {email, name, password} — sets a password too; choosing this at setup is
//     the opt-in, so it also turns access.password_login on. Older CLIs only know
//     this shape, and it keeps working.
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
			Code     string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := data.NormalizeEmail(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}

		var user *data.User
		var err error
		switch {
		case in.Code != "":
			key := data.SetupOTPKey(email)
			bucket := "otp-fail:" + key
			if db.RateLimitExceeded(bucket, loginFailLimit, loginFailWindow) {
				jsonError(w, "too many wrong codes — request a new one later", http.StatusTooManyRequests)
				return
			}
			if !db.VerifyOTP(key, strings.TrimSpace(in.Code)) {
				db.RateLimitHit(bucket, loginFailWindow)
				jsonError(w, "invalid or expired code", http.StatusUnauthorized)
				return
			}
			db.RateLimitClear(bucket)
			user, err = db.CreateMember(email, name, "owner")
		case in.Password != "":
			if len(in.Password) < 8 {
				jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
				return
			}
			user, err = db.CreateUser(email, name, in.Password, "owner")
			if err == nil {
				db.SetSetting(settingPasswordLogin, "true")
			}
		default:
			jsonError(w, "enter the code we sent to your email (POST /setup/request-code first)", http.StatusBadRequest)
			return
		}
		if err != nil {
			jsonError(w, "could not create account: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent()); err == nil {
			setSessionCookie(w, r, token)
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

// handleMigrate upgrades a legacy single-admin password into the first owner
// account. It runs before any owner exists, so no session can gate it; the
// legacy password itself is the proof, checked with the same lockout as login.
func handleMigrate(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.HasLegacyAdmin() {
			jsonError(w, "no legacy admin to migrate", http.StatusBadRequest)
			return
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := data.NormalizeEmail(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		const bucket = "migrate-fail"
		if db.RateLimitExceeded(bucket, loginFailLimit, loginFailWindow) {
			jsonError(w, "too many failed attempts — try again later", http.StatusTooManyRequests)
			return
		}
		if !db.VerifyLegacyAdminPassword(in.Password) {
			db.RateLimitHit(bucket, loginFailWindow)
			jsonError(w, "that isn't the existing admin password", http.StatusUnauthorized)
			return
		}
		db.RateLimitClear(bucket)
		// The upgraded owner only knows a password, so passwords stay allowed.
		db.SetSetting(settingPasswordLogin, "true")
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
// code is delivered by email. Configure with RESEND_API_KEY + FRIENDO_EMAIL_FROM.
func emailConfigured() bool {
	return email.Configured()
}

// otpEchoEnabled reports whether request-code may return the login code in its
// response — the shared DEV-ONLY rule in the email package (explicit
// FRIENDO_OTP_ECHO and no real provider configured).
func otpEchoEnabled() bool {
	return email.EchoEnabled()
}

// sendOTPEmail delivers a login code via the shared email package (Resend).
func sendOTPEmail(to, code string) {
	email.SendLoginCode(to, code)
}

func generateOTP() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// handleRequestCode issues a one-time login code for an email. An email with
// no account yet gets its code keyed on the email itself (data.SignupOTPKey):
// the account is only made once the code checks out, in handleVerifyCode, so
// asking for a code never leaves an unverified account behind.
func handleRequestCode(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		email := data.NormalizeEmail(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}

		key := data.SignupOTPKey(email)
		if user, err := db.GetUserByEmail(email); err == nil {
			key = user.ID
		} else if !db.GetBoolSetting(settingOpenSignups, true) {
			// New self-serve account. Honor the site's signup policy.
			jsonError(w, "sign-ups are disabled for this site", http.StatusForbidden)
			return
		}

		// Rate limit: one live code at a time per email within the window.
		if db.HasFreshOTP(key, otpResendWindow) {
			jsonError(w, "a code was already sent — please wait before requesting another", http.StatusTooManyRequests)
			return
		}

		code := generateOTP()
		if err := db.CreateOTP(key, code, 10*time.Minute); err != nil {
			jsonError(w, "could not issue code", http.StatusInternalServerError)
			return
		}
		sendOTPEmail(email, code)

		resp := map[string]any{"sent": true, "emailed": emailConfigured()}
		// Echo the code only in explicit dev mode and only when no real email
		// provider is configured — never on a production/managed site.
		if otpEchoEnabled() {
			resp["code"] = code
		}
		jsonResponse(w, resp)
	}
}

// handleVerifyCode validates a one-time code and starts a session. Wrong
// guesses are capped per account (same lockout as password login) so a
// six-digit code can't be brute-forced inside its ten-minute life.
//
// A code for a new email makes the account now. If this browser was a visitor,
// that visitor account becomes the new one (same account, so everything they
// did stays theirs); signing in to an existing account instead moves the
// visitor's activity onto it.
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
		email := data.NormalizeEmail(in.Email)
		code := strings.TrimSpace(in.Code)
		bucket := "otp-fail:" + email
		if db.RateLimitExceeded(bucket, loginFailLimit, loginFailWindow) {
			jsonError(w, "too many wrong codes — request a new one later", http.StatusTooManyRequests)
			return
		}
		user, err := db.GetUserByEmail(email)
		verified := false
		if err == nil {
			// An account made after the code was sent (an admin added it
			// meanwhile) still honors the code sent to the bare email.
			verified = db.VerifyOTP(user.ID, code) || db.VerifyOTP(data.SignupOTPKey(email), code)
		} else if email != "" {
			verified = db.VerifyOTP(data.SignupOTPKey(email), code)
		}
		if !verified {
			db.RateLimitHit(bucket, loginFailWindow)
			jsonError(w, "invalid or expired code", http.StatusUnauthorized)
			return
		}
		db.RateLimitClear(bucket)
		if user != nil {
			carryVisitor(r, db, user)
		} else {
			if !db.GetBoolSetting(settingOpenSignups, true) {
				jsonError(w, "sign-ups are disabled for this site", http.StatusForbidden)
				return
			}
			role := signupRole(db)
			if role != "member" && role != "contributor" {
				role = "member"
			}
			if v, _ := sessionVisitor(r, db); v != nil {
				user, err = db.PromoteVisitor(v.ID, email, role)
			} else {
				user, err = db.CreateMember(email, "", role)
			}
			if err != nil {
				jsonError(w, "could not create account: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		token, err := db.CreateSession(user.ID, r.RemoteAddr, r.UserAgent())
		if err != nil {
			jsonError(w, "could not create session", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, r, token)
		jsonResponse(w, map[string]any{"user": userJSON(user)})
	}
}

// --- Users ---

// canAssignRole reports whether an actor may assign targetRole. Granting admin or
// owner requires site.own (owners only); the lower roles require user.manage.
func canAssignRole(actorRole, targetRole string) bool {
	if !data.ValidRole(targetRole) {
		return false
	}
	if targetRole == "owner" || targetRole == "admin" {
		return data.RoleCan(actorRole, data.CapSiteOwn)
	}
	return data.RoleCan(actorRole, data.CapUserManage)
}

// membersOnly drops visitor accounts from a user list: the admin's Users page
// and its count are about people with accounts. (Export and pull keep them,
// so a moved site keeps what its visitors did.)
func membersOnly(users []*data.User, err error) []*data.User {
	if err != nil {
		return nil
	}
	out := users[:0:0]
	for _, u := range users {
		if !u.IsVisitor() {
			out = append(out, u)
		}
	}
	return out
}

func handleListUsers(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := db.ListUsers()
		users := membersOnly(all, err)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		out := []map[string]any{}
		for _, u := range users {
			m := userJSON(u)
			// The address of the profile attribution flows to, so the admin can
			// link to a person's profile page.
			if p, err := db.ProfileByID(db.DefaultAuthorID(u.ID)); err == nil {
				m["profile_slug"] = p["slug"]
			}
			out = append(out, m)
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
		email := data.NormalizeEmail(in.Email)
		if email == "" {
			jsonError(w, "email is required", http.StatusBadRequest)
			return
		}
		// A password is optional: everyone can sign in with an emailed code, so
		// an account without one is complete. If one is given it must be usable.
		if in.Password != "" && len(in.Password) < 8 {
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
		var user *data.User
		var err error
		if in.Password == "" {
			user, err = db.CreateMember(email, name, role)
		} else {
			user, err = db.CreateUser(email, name, in.Password, role)
		}
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
		// Can't modify someone at or above your own rank — unless you're an owner
		// (owners may manage other owners, enabling co-owner changes + step-down).
		if actor.Role != "owner" && data.RoleRank(target.Role) >= data.RoleRank(actor.Role) {
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
			// Last-owner guard: never demote the site's only owner.
			if target.Role == "owner" && db.CountOwners() <= 1 {
				jsonError(w, "cannot demote the last owner", http.StatusConflict)
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
		if actor.Role != "owner" && data.RoleRank(target.Role) >= data.RoleRank(actor.Role) {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
		// Last-owner guard: never delete the site's only owner.
		if target.Role == "owner" && db.CountOwners() <= 1 {
			jsonError(w, "cannot delete the last owner", http.StatusConflict)
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

// Setting keys. One flat name each: the same word in friendo.toml's [settings]
// block, in the database, in the admin and in the settings API.
const (
	// Anyone can sign up (else only accounts an admin adds).
	settingOpenSignups = "open_signups"
	// New members start as contributors (else as plain members).
	settingSignupsAreContributors = "signups_are_contributors"
	// Members can post from a <friendo-form>; their posts wait for review.
	settingMembersCanPost = "members_can_post"
	// Contributors' posts wait for review before they go live.
	settingPostsNeedReview = "posts_need_review"
	// Comments wait for review before they show (default on).
	settingCommentsNeedReview = "comments_need_review"
	// Password sign-in is allowed too. Off by default: everyone can always
	// sign in with a code emailed to them.
	settingPasswordLogin = "password_login"
)

// tomlSettingsConfig mirrors the [settings] block of friendo.toml. Pointer fields
// distinguish "declared" from "absent" so only the keys the author actually wrote
// are treated as managed.
type tomlSettingsConfig struct {
	Settings struct {
		OpenSignups            *bool `toml:"open_signups"`
		SignupsAreContributors *bool `toml:"signups_are_contributors"`
		MembersCanPost         *bool `toml:"members_can_post"`
		PostsNeedReview        *bool `toml:"posts_need_review"`
		CommentsNeedReview     *bool `toml:"comments_need_review"`
		PasswordLogin          *bool `toml:"password_login"`
		// Who may see profiles: "members" (default) or "public".
		ProfileVisibility *string `toml:"profile_visibility"`
		// Whether plain members may start groups (default off).
		MembersCanStartGroups *bool `toml:"members_can_start_groups"`
		// What a visitor (not signed in) may do (each default off).
		VisitorsCanReact *bool `toml:"visitors_can_react"`
		VisitorsCanVote  *bool `toml:"visitors_can_vote"`
		VisitorsCanRSVP  *bool `toml:"visitors_can_rsvp"`
		// Feature switches (see data.Features) and the built-in collections a
		// site without [content] collections shows.
		Comments           *bool    `toml:"comments"`
		Reactions          *bool    `toml:"reactions"`
		Polls              *bool    `toml:"polls"`
		RSVP               *bool    `toml:"rsvp"`
		Locations          *bool    `toml:"locations"`
		Chats              *bool    `toml:"chats"`
		Follows            *bool    `toml:"follows"`
		Groups             *bool    `toml:"groups"`
		DefaultCollections []string `toml:"default_collections"`
	} `toml:"settings"`
}

// applyManagedSettings makes friendo.toml the source of truth for the settings its
// [settings] block declares: each declared key is written into the DB (overwriting
// any admin edit) and added to the returned set, which the settings API uses to
// report them and to refuse changes. Keys the author omits stay admin-managed.
// Applied once at mount time — editing friendo.toml takes effect on the next start.
func applyManagedSettings(db *data.DB, siteDir string) map[string]bool {
	managed := map[string]bool{}
	if siteDir == "" {
		return managed
	}
	path := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(path); err != nil {
		return managed
	}
	var cfg tomlSettingsConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		log.Printf("friendo.toml: could not read [settings]: %v", err)
		return managed
	}
	s := cfg.Settings
	set := func(key, val string) {
		db.SetSetting(key, val)
		managed[key] = true
	}
	for key, v := range map[string]*bool{
		settingOpenSignups: s.OpenSignups, settingSignupsAreContributors: s.SignupsAreContributors,
		settingMembersCanPost: s.MembersCanPost, settingPostsNeedReview: s.PostsNeedReview,
		settingCommentsNeedReview: s.CommentsNeedReview, settingPasswordLogin: s.PasswordLogin,
		data.SettingMembersCanStartGroups: s.MembersCanStartGroups,
		settingVisitorsCanReact: s.VisitorsCanReact, settingVisitorsCanVote: s.VisitorsCanVote,
		settingVisitorsCanRSVP: s.VisitorsCanRSVP,
	} {
		if v != nil {
			set(key, boolSetting(*v))
		}
	}
	if s.ProfileVisibility != nil {
		if *s.ProfileVisibility == "members" || *s.ProfileVisibility == "public" {
			set(data.SettingProfileVisibility, *s.ProfileVisibility)
		} else {
			log.Printf("friendo.toml: ignoring invalid [settings] profile_visibility %q (want \"members\" or \"public\")", *s.ProfileVisibility)
		}
	}
	for name, v := range map[string]*bool{
		"comments": s.Comments, "reactions": s.Reactions, "polls": s.Polls,
		"rsvp": s.RSVP, "locations": s.Locations, "chats": s.Chats, "follows": s.Follows, "groups": s.Groups,
	} {
		if v != nil {
			set(data.FeatureSetting(name), boolSetting(*v))
		}
	}
	if s.DefaultCollections != nil {
		set(settingDefaultCollections, strings.Join(normalizeDefaultCollections(s.DefaultCollections), ","))
	}
	return managed
}

// boolSettings lists the on/off settings the API reads and writes by name.
var boolSettings = []struct {
	key string
	def bool
}{
	{settingOpenSignups, true},
	{settingSignupsAreContributors, false},
	{settingMembersCanPost, false},
	{settingPostsNeedReview, false},
	{settingCommentsNeedReview, true},
	{settingPasswordLogin, false},
	{data.SettingMembersCanStartGroups, false},
	{settingVisitorsCanReact, false},
	{settingVisitorsCanVote, false},
	{settingVisitorsCanRSVP, false},
}

// managedList returns the managed setting keys in a stable order for the API, so the
// admin SPA can render those controls read-only.
func managedList(managed map[string]bool) []string {
	order := []string{}
	for _, b := range boolSettings {
		order = append(order, b.key)
	}
	order = append(order, data.SettingProfileVisibility, settingDefaultCollections)
	for _, f := range data.Features {
		order = append(order, data.FeatureSetting(f))
	}
	out := []string{}
	for _, k := range order {
		if managed[k] {
			out = append(out, k)
		}
	}
	return out
}

func boolSetting(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// commentsNeedReview reports whether a new comment waits for review (the default).
func commentsNeedReview(db *data.DB) bool {
	return db.GetBoolSetting(settingCommentsNeedReview, true)
}

// signupRole is what a self-serve sign-up becomes.
func signupRole(db *data.DB) string {
	if db.GetBoolSetting(settingSignupsAreContributors, false) {
		return "contributor"
	}
	return "member"
}

// settingsPayload builds the settings object returned by GET/PUT /settings: every
// setting by its one name, plus the site's counts, the feature switches and the
// `managed` list naming the keys frozen by friendo.toml's [settings] block.
func settingsPayload(db *data.DB, siteName string, managed map[string]bool, collectionsDeclared bool) map[string]any {
	users := membersOnly(db.ListUsers())
	counts, _ := db.CollectionCounts()
	out := map[string]any{
		"site":                 map[string]any{"name": siteName},
		"collections":          len(counts),
		"users":                len(users),
		"profile_visibility":   db.ProfileVisibility(),
		"default_collections":  defaultCollectionsFor(db),
		"collections_declared": collectionsDeclared,
		"features":             featuresMap(db),
		"managed":              managedList(managed),
	}
	for _, b := range boolSettings {
		out[b.key] = db.GetBoolSetting(b.key, b.def)
	}
	return out
}

func handleSettings(db *data.DB, siteName string, managed map[string]bool, collectionsDeclared bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, settingsPayload(db, siteName, managed, collectionsDeclared))
	}
}

// handleUpdateSettings persists admin-configurable settings, each by its one
// name. Keys frozen by friendo.toml's [settings] block are silently skipped — the
// SPA disables them, and this keeps the DB from drifting from the file (which
// would just overwrite it on the next start anyway).
func handleUpdateSettings(db *data.DB, siteName string, managed map[string]bool, collectionsDeclared bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		for _, b := range boolSettings {
			raw, ok := in[b.key]
			if !ok || managed[b.key] {
				continue
			}
			var v bool
			if err := json.Unmarshal(raw, &v); err != nil {
				jsonError(w, b.key+" must be true or false", http.StatusBadRequest)
				return
			}
			db.SetSetting(b.key, boolSetting(v))
		}
		if raw, ok := in[data.SettingProfileVisibility]; ok && !managed[data.SettingProfileVisibility] {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil || (v != "members" && v != "public") {
				jsonError(w, "profile_visibility must be members or public", http.StatusBadRequest)
				return
			}
			db.SetSetting(data.SettingProfileVisibility, v)
		}
		if raw, ok := in[settingDefaultCollections]; ok && !managed[settingDefaultCollections] {
			var v []string
			if err := json.Unmarshal(raw, &v); err != nil {
				jsonError(w, "default_collections must be a list", http.StatusBadRequest)
				return
			}
			db.SetSetting(settingDefaultCollections, strings.Join(normalizeDefaultCollections(v), ","))
		}
		if raw, ok := in["features"]; ok {
			var features map[string]*bool
			if err := json.Unmarshal(raw, &features); err != nil {
				jsonError(w, "features must be an object of switches", http.StatusBadRequest)
				return
			}
			for name, v := range features {
				if v == nil || data.FeatureLabel[name] == "" || managed[data.FeatureSetting(name)] {
					continue
				}
				db.SetSetting(data.FeatureSetting(name), boolSetting(*v))
			}
		}
		jsonResponse(w, settingsPayload(db, siteName, managed, collectionsDeclared))
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
