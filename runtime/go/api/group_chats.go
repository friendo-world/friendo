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

// Group chats. A group's chats are its own: <friendo-chat chat-id="general"
// group="board"> names one (a group may have many; keys are unique within a
// group), and only the group's members may read, stream or post in it —
// site moderators and up too. A group's admins create and delete its chats;
// its moderators may delete any message in them. Behind both the chats and the
// groups switches.
//
//	GET    /_/api/groups/{id}/chats                    the group's chats
//	POST   /_/api/groups/{id}/chats {id, name}        moderator adds one
//	DELETE /_/api/groups/{id}/chats/{chat}              moderator removes one
//	GET    /_/api/groups/{id}/chats/{chat}/messages     members
//	POST   /_/api/groups/{id}/chats/{chat}/messages     members
//	GET    /_/api/groups/{id}/chats/{chat}/stream       members (SSE)

// chatAccess is what a viewer may do in one chat.
type chatAccess struct {
	canRead, canPost, canModerate bool
}

// chatAccessFor applies the chat's rules to a viewer: a site-wide chat is public
// to read and open to any member to post, moderated by the site's moderators; a
// group's chat is its members' (site moderators and up count as members),
// moderated by the group's moderators and the site's.
func chatAccessFor(db *data.DB, user *data.User, chat map[string]any) chatAccess {
	groupID, _ := chat["group_id"].(string)
	if groupID == "" {
		return chatAccess{canRead: true, canPost: user != nil, canModerate: user != nil && user.Can(data.CapReviewAny)}
	}
	if user == nil {
		return chatAccess{}
	}
	authorID := db.DefaultAuthorID(user.ID)
	member := user.Can(data.CapReviewAny) || db.IsGroupMember(groupID, authorID)
	if !member {
		return chatAccess{}
	}
	return chatAccess{canRead: true, canPost: true, canModerate: user.Can(data.CapReviewAny) || db.IsGroupModerator(groupID, authorID)}
}

// loadGroupChat resolves {id} (a group) and {key} (one of its chats) and applies
// the membership rule: 401 signed out, 403 signed in but not a member, 404 for
// an unknown group (or one the viewer can't see) or chat.
func loadGroupChat(db *data.DB, w http.ResponseWriter, r *http.Request, user *data.User) (map[string]any, chatAccess, bool) {
	v := groupViewerFor(db, user)
	g, ok := loadGroup(db, w, r, v)
	if !ok {
		return nil, chatAccess{}, false
	}
	chat, err := db.GetGroupChat(g["id"].(string), chi.URLParam(r, "chat"))
	if err == sql.ErrNoRows {
		jsonError(w, "chat not found", http.StatusNotFound)
		return nil, chatAccess{}, false
	}
	if err != nil {
		jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
		return nil, chatAccess{}, false
	}
	access := chatAccessFor(db, user, chat)
	if !access.canRead {
		if user == nil {
			jsonError(w, "sign in and join the group to see this chat", http.StatusUnauthorized)
		} else {
			jsonError(w, "join the group to see this chat", http.StatusForbidden)
		}
		return nil, chatAccess{}, false
	}
	return chat, access, true
}

func handleListGroupChats(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := groupViewerFor(db, authFunc(r))
		g, ok := loadGroup(db, w, r, v)
		if !ok {
			return
		}
		chats, err := db.ListGroupChats(g["id"].(string))
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"chats": chats})
	}
}

func handleCreateGroupChat(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
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
			Key  string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		in.Key, in.Name = strings.TrimSpace(in.Key), strings.TrimSpace(in.Name)
		if in.Key == "" && in.Name != "" {
			in.Key = data.Slugify(in.Name)
		}
		if !data.ValidChatID(in.Key) {
			jsonError(w, "a chat id is letters, digits, dots, dashes or underscores (up to 64)", http.StatusBadRequest)
			return
		}
		if _, err := db.GetGroupChat(id, in.Key); err == nil {
			jsonError(w, "this group already has a chat with that id", http.StatusConflict)
			return
		}
		if _, err := db.CreateGroupChat(id, in.Key, in.Name); err != nil {
			jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
			return
		}
		chats, _ := db.ListGroupChats(id)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"chats": chats})
	}
}

func handleDeleteGroupChat(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
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
		chat, err := db.GetGroupChat(id, chi.URLParam(r, "chat"))
		if err != nil {
			jsonError(w, "chat not found", http.StatusNotFound)
			return
		}
		if err := db.DeleteChat(data.ChatRowID(chat)); err != nil {
			jsonError(w, fmt.Sprintf("delete error: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListGroupChatMessages(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		chat, access, ok := loadGroupChat(db, w, r, user)
		if !ok {
			return
		}
		serveMessages(db, w, user, data.ChatRowID(chat), access)
	}
}

func handlePostGroupChatMessage(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		chat, _, ok := loadGroupChat(db, w, r, user)
		if !ok {
			return
		}
		postMessage(db, w, r, user, data.ChatRowID(chat))
	}
}

func handleGroupChatStream(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chat, _, ok := loadGroupChat(db, w, r, authFunc(r))
		if !ok {
			return
		}
		streamChat(w, r, data.ChatRowID(chat))
	}
}
