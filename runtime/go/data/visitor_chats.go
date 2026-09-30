package data

import (
	"sort"
	"strings"
)

// settingVisitorChats holds the chat-id patterns the site's templates mark
// visitors-can-chat (see renderer.VisitorChatsInTemplate), one per line — a
// templated id may hold a comma. Rescanned whenever the site's pages load.
const settingVisitorChats = "visitor_chats_in_pages"

// VisitorChatPatterns returns the chat-id patterns visitors may write in.
func (db *DB) VisitorChatPatterns() []string {
	v := db.GetSetting(settingVisitorChats, "")
	if v == "" {
		return nil
	}
	return strings.Split(v, "\n")
}

// SetVisitorChatPatterns records a template scan's patterns and returns the
// ones that were added and removed.
func (db *DB) SetVisitorChatPatterns(patterns []string) (added, removed []string) {
	was := map[string]bool{}
	for _, p := range db.VisitorChatPatterns() {
		was[p] = true
	}
	now := map[string]bool{}
	for _, p := range patterns {
		now[p] = true
		if !was[p] {
			added = append(added, p)
		}
	}
	for p := range was {
		if !now[p] {
			removed = append(removed, p)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}
	list := make([]string, 0, len(now))
	for p := range now {
		list = append(list, p)
	}
	sort.Strings(list)
	sort.Strings(removed)
	db.SetSetting(settingVisitorChats, strings.Join(list, "\n"))
	return added, removed
}
