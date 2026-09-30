package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/renderer"
)

// Chats visitors may write in: <friendo-chat chat-id="general" visitors-can-chat>
// in a template (read from the files when the site loads; see
// server/template_scan.go and renderer.VisitorChatsInTemplate). A templated id,
// chat-id="post-{{ post.slug }}", opens every chat whose id fits it. A visitor
// gives a name before their first message; their messages show "(visitor)",
// go live like everyone's, and are theirs to delete. A group's chat is always
// for its members.

// chatOpenToVisitors reports whether the site's templates let visitors write
// in a site-wide chat.
func chatOpenToVisitors(db *data.DB, chatID string) bool {
	for _, p := range db.VisitorChatPatterns() {
		if re, err := renderer.ChatPatternRegexp(p); err == nil && re.MatchString(chatID) {
			return true
		}
	}
	return false
}

// visitorMessageChecks runs before a visitor's message is saved: the trap
// field bots fill (202, nothing saved) and the name a visitor gives the first
// time (400 with needs_name until they have one). It answers and returns false
// when the message shouldn't go on.
func visitorMessageChecks(w http.ResponseWriter, db *data.DB, user *data.User, name, trap string) bool {
	if !user.IsVisitor() {
		return true
	}
	if trapped(w, user, trap) {
		return false
	}
	needsName := func(msg string) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": msg, "needs_name": true})
		return false
	}
	if strings.TrimSpace(name) != "" {
		if err := db.SetVisitorName(user.ID, name); err != nil {
			return needsName(err.Error())
		}
		return true
	}
	if db.VisitorName(user.ID) == "" {
		return needsName("add your name so everyone knows who's talking")
	}
	return true
}
