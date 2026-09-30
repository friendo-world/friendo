package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Notifications API (v0.6 Tier D): the signed-in account's inbox.
//
//	GET /_/api/me/notifications[?unread=1&limit=n]   {notifications, unread}
//	PUT /_/api/me/notifications/{id}/read             {notification}
//	PUT /_/api/me/notifications/read-all              {unread: 0}
//
// The inbox is on while follows or groups is; off, it answers 403 off:true like
// any switched-off feature so <friendo-inbox> and the badge render nothing.

func notificationsGate(db *data.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.NotificationsOn() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"Notifications are turned off for this site","off":true}`)
			return
		}
		next(w, r)
	}
}

// describeTarget fills a notification's target from its type + id: a post's
// title and page, a comment's post, a profile's profile page.
func describeTarget(db *data.DB, permalink data.PermalinkFunc, n *data.Notification) {
	t := map[string]any{"type": n.TargetType, "id": n.TargetID}
	n.Target = t
	postURL := func(record map[string]any) string {
		collection, _ := record["collection"].(string)
		slug, _ := record["slug"].(string)
		if permalink != nil {
			if u := permalink(collection, map[string]string{"slug": slug, "id": record["id"].(string)}); u != "" {
				return u
			}
		}
		return "/" + collection + "/" + slug
	}
	switch n.TargetType {
	case "post":
		if record, err := db.GetRecordByID(n.TargetID); err == nil {
			t["title"], t["collection"], t["slug"] = record["title"], record["collection"], record["slug"]
			t["url"] = postURL(record)
		}
	case "comment":
		if c, err := db.GetComment(n.TargetID); err == nil {
			t["body"] = c["body"]
			if postID, _ := c["post_id"].(string); postID != "" {
				if record, err := db.GetRecordByID(postID); err == nil {
					t["title"], t["collection"], t["slug"] = record["title"], record["collection"], record["slug"]
					t["url"] = postURL(record)
				}
			}
		}
	case "author":
		if p, err := db.ProfileByID(n.TargetID); err == nil {
			t["title"], t["url"] = p["name"], p["url"]
		}
	}
}

func handleListNotifications(db *data.DB, authFunc func(*http.Request) *data.User, permalink data.PermalinkFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := db.ListNotifications(user.ID, r.URL.Query().Get("unread") != "", limit)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		for _, n := range list {
			describeTarget(db, permalink, n)
		}
		jsonResponse(w, map[string]any{"notifications": list, "unread": db.UnreadCount(user.ID)})
	}
}

func handleMarkNotificationRead(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		err := db.MarkRead(user.ID, chi.URLParam(r, "id"))
		if err == sql.ErrNoRows {
			jsonError(w, "notification not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "unread": db.UnreadCount(user.ID)})
	}
}

func handleMarkAllNotificationsRead(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if user == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := db.MarkAllRead(user.ID); err != nil {
			jsonError(w, fmt.Sprintf("update error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "unread": 0})
	}
}

// notifyCommentOnPost tells a post's author about a comment that's visible
// (approved). Called on create when auto-approve is on, and on approval.
func notifyCommentOnPost(db *data.DB, commentID string) {
	c, err := db.GetComment(commentID)
	if err != nil {
		return
	}
	postID, _ := c["post_id"].(string)
	actor, _ := c["author_id"].(string)
	// An anonymous comment tells the post's author that someone commented, not who.
	if c["anonymous"] == true {
		actor = ""
	}
	record, err := db.GetRecordByID(postID)
	if err != nil {
		return
	}
	recipient, _ := record["author_id"].(string)
	db.Notify(recipient, data.NotifyComment, "comment", commentID, actor)
}
