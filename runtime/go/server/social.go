package server

import (
	"github.com/friendo-world/friendo/runtime/go/data"
)

// Group visibility on the page (v0.6 Tier C).
//
// collections.* is built for every request, so this is where a private group
// and the posts filed under it disappear for someone who isn't in it — one
// membership query per signed-in viewer, then filtering in memory. Off (the
// groups switch), nothing is hidden and record.group is nil everywhere.

// groupViewer is what the visibility rules need to know about the viewer.
type groupViewer struct {
	signedIn bool
	memberOf map[string]bool // group ids
}

func viewerGroups(db *data.DB, user *data.User) groupViewer {
	v := groupViewer{signedIn: user != nil, memberOf: map[string]bool{}}
	if user != nil && db.FeatureOn("groups") {
		if authorID := db.DefaultAuthorID(user.ID); authorID != "" {
			v.memberOf = db.MemberOfSet(authorID)
		}
	}
	return v
}

// applyGroupVisibility trims a collections context for a viewer: groups they
// can't see are dropped from collections.groups, records filed under those
// groups (data.group) are dropped from every other collection, and every
// remaining record gets record.group (its group's record, or nil).
func applyGroupVisibility(db *data.DB, cols map[string]any, viewer groupViewer) {
	groups, _ := cols[data.GroupsCollection].([]map[string]any)
	on := db.FeatureOn("groups")
	visible := []map[string]any{}
	hidden := map[string]bool{}
	for _, g := range groups {
		if !on || data.CanSeeGroup(g, viewer.signedIn, viewer.memberOf) {
			visible = append(visible, g)
		} else if slug, _ := g["slug"].(string); slug != "" {
			hidden[slug] = true
		}
	}
	if groups != nil {
		cols[data.GroupsCollection] = visible
	}
	for name, v := range cols {
		records, ok := v.([]map[string]any)
		if !ok || name == data.GroupsCollection || name == "profiles" {
			continue
		}
		if len(hidden) > 0 {
			kept := records[:0]
			for _, r := range records {
				if !hidden[data.GroupSlugOf(r)] {
					kept = append(kept, r)
				}
			}
			records = kept
			cols[name] = records
		}
		if on {
			data.AttachGroups(records, visible)
		} else {
			data.AttachGroups(records, nil)
		}
	}
	if on {
		for _, g := range visible {
			attachGroupRelations(db, g)
		}
	}
}

// attachGroupRelations adds what a group's page shows: record.settings
// ({visibility, join}), record.members (profiles, admins and moderators first),
// record.admins, record.moderators and record.member_count.
func attachGroupRelations(db *data.DB, group map[string]any) {
	id, _ := group["id"].(string)
	visibility, join := data.GroupSettings(group)
	group["settings"] = map[string]any{"visibility": visibility, "join": join}
	members, err := db.ListMembers(id, "")
	if err != nil {
		members = []map[string]any{}
	}
	group["members"] = members
	admins, mods := []map[string]any{}, []map[string]any{}
	for _, m := range members {
		switch m["role"] {
		case data.GroupRoleAdmin:
			admins = append(admins, m)
		case data.GroupRoleModerator:
			mods = append(mods, m)
		}
	}
	group["admins"] = admins
	group["moderators"] = mods
	group["member_count"] = len(members)
	// The group's chats ({% for c in group.chats %}<friendo-chat chat-id="{{ c.id }}" group="…">).
	group["chats"] = []map[string]any{}
	if db.FeatureOn("chats") {
		if chats, err := db.ListGroupChats(id); err == nil {
			group["chats"] = chats
		}
	}
}

// groupBlock decides whether a focused record is hidden from the viewer by a
// group: the record is a group they can't see, or is filed under one. It returns
// (blocked, signIn): signIn means show the sign-in page (a visitor), else a 404
// (a member who isn't in the group — the page doesn't confirm it exists).
func groupBlock(db *data.DB, record map[string]any, collection string, viewer groupViewer, visibleGroups []map[string]any) (blocked, signIn bool) {
	if !db.FeatureOn("groups") {
		return false, false
	}
	var group map[string]any
	if collection == data.GroupsCollection {
		group = record
	} else if slug := data.GroupSlugOf(record); slug != "" {
		if g, err := db.GroupBySlug(slug); err == nil {
			group = g
		}
	}
	if group == nil {
		return false, false
	}
	if data.CanSeeGroup(group, viewer.signedIn, viewer.memberOf) {
		return false, false
	}
	return true, !viewer.signedIn
}
