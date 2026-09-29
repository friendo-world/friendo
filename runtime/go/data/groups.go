package data

import (
	"database/sql"
	"time"
)

// Groups (v0.6 Tier C).
//
// A group is a post in the `groups` collection, the way an event is a post with
// a `when`: it has a title, a slug, a body, an author (its founder), and its
// settings are two keys in `data` — `visibility` (public | members | private) and
// `join` (open | request | invite) — beside whatever [content.groups.fields] a site
// declares. Membership is the one new table. A post belongs to a group by
// reference: `group: board` in its front matter (a plain string compare — a
// value that names no group is just data, which is what the docs site's own
// `group: Concepts` has always been).

// GroupsCollection is the collection name groups live in.
const GroupsCollection = "groups"

// SettingMembersCanStartGroups says whether plain members may start groups
// (anyone holding content.create always can). Default off.
const SettingMembersCanStartGroups = "members_can_start_groups"

// Membership roles and states. A group mirrors the site: its admins run it
// (settings, deleting, promoting), its moderators keep it tidy (requests,
// removals, messages), everyone else is a member.
const (
	GroupRoleAdmin     = "admin"
	GroupRoleModerator = "moderator"
	GroupRoleMember    = "member"

	MembershipMember    = "member"
	MembershipRequested = "requested"
	MembershipInvited   = "invited"
)

// GroupVisibilities and GroupJoins are the settings a group's data may hold.
var GroupVisibilities = map[string]bool{"public": true, "members": true, "private": true}
var GroupJoins = map[string]bool{"open": true, "request": true, "invite": true}

// MembersCanStartGroups reports whether plain members may start groups.
func (db *DB) MembersCanStartGroups() bool {
	return db.GetBoolSetting(SettingMembersCanStartGroups, false)
}

// GroupSettings reads a group record's visibility and join rule from its data,
// with public / open as the defaults and anything unknown read as the default.
func GroupSettings(record map[string]any) (visibility, join string) {
	visibility, join = "public", "open"
	d, _ := record["fields"].(map[string]any)
	if v, _ := d["visibility"].(string); GroupVisibilities[v] {
		visibility = v
	}
	if j, _ := d["join"].(string); GroupJoins[j] {
		join = j
	}
	return
}

// GroupSlugOf returns the group a record says it belongs to (data.group), or "".
func GroupSlugOf(record map[string]any) string {
	d, _ := record["fields"].(map[string]any)
	s, _ := d["group"].(string)
	return s
}

// GroupBySlug returns a published group record by slug, or sql.ErrNoRows.
func (db *DB) GroupBySlug(slug string) (map[string]any, error) {
	record, err := db.QueryCollectionByField(GroupsCollection, "slug", slug)
	if err != nil {
		return nil, sql.ErrNoRows
	}
	if s, _ := record["status"].(string); s != "published" {
		return nil, sql.ErrNoRows
	}
	record["collection"] = GroupsCollection
	return record, nil
}

// GroupByID returns a published group record by id, or sql.ErrNoRows.
func (db *DB) GroupByID(id string) (map[string]any, error) {
	record, err := db.GetRecordByID(id)
	if err != nil {
		return nil, err
	}
	if c, _ := record["collection"].(string); c != GroupsCollection {
		return nil, sql.ErrNoRows
	}
	if s, _ := record["status"].(string); s != "published" {
		return nil, sql.ErrNoRows
	}
	return record, nil
}

// Membership returns a profile's row in a group: role, status, and whether one exists.
func (db *DB) Membership(groupID, authorID string) (role, status string, ok bool) {
	err := db.Conn.QueryRow(
		`SELECT role, status FROM memberships WHERE site_id = ? AND group_id = ? AND author_id = ?`,
		db.SiteID, groupID, authorID,
	).Scan(&role, &status)
	return role, status, err == nil
}

// SetMembership writes a profile's row in a group (insert or update).
func (db *DB) SetMembership(groupID, authorID, role, status string) error {
	if groupID == "" || authorID == "" {
		return nil
	}
	if !ValidGroupRole(role) {
		role = GroupRoleMember
	}
	if status != MembershipRequested && status != MembershipInvited {
		status = MembershipMember
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.Conn.Exec(
		`INSERT INTO memberships (id, site_id, group_id, author_id, role, status, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(site_id, group_id, author_id) DO UPDATE SET role = excluded.role, status = excluded.status, updated = excluded.updated`,
		GenerateID(), db.SiteID, groupID, authorID, role, status, now, now,
	)
	return err
}

// RemoveMembership deletes a profile's row in a group (leave, withdraw, remove).
func (db *DB) RemoveMembership(groupID, authorID string) error {
	_, err := db.Conn.Exec(`DELETE FROM memberships WHERE site_id = ? AND group_id = ? AND author_id = ?`, db.SiteID, groupID, authorID)
	return err
}

// ListMembers returns a group's rows in one state ("" = member), moderators
// first, each a profile map plus `role`, `status` and `since`.
func (db *DB) ListMembers(groupID, status string) ([]map[string]any, error) {
	if status == "" {
		status = MembershipMember
	}
	rows, err := db.Conn.Query(
		`SELECT `+prefixed(profileCols, "a.")+`, m.role, m.status, m.created FROM memberships m
		 JOIN authors a ON a.id = m.author_id AND a.site_id = m.site_id
		 WHERE m.site_id = ? AND m.group_id = ? AND m.status = ?
		 ORDER BY CASE m.role WHEN 'moderator' THEN 0 ELSE 1 END, m.created, m.rowid`,
		db.SiteID, groupID, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, avatar, arole, slug, bio, dataJSON, created, role, st, since string
		if err := rows.Scan(&id, &name, &avatar, &arole, &slug, &bio, &dataJSON, &created, &role, &st, &since); err != nil {
			return nil, err
		}
		p := profileMap(id, name, avatar, arole, slug, bio, dataJSON, created)
		p["role"], p["status"], p["since"] = role, st, since
		out = append(out, p)
	}
	return out, rows.Err()
}

// MemberCount is how many profiles are members (not requested/invited) of a group.
func (db *DB) MemberCount(groupID string) int {
	var n int
	db.Conn.QueryRow(`SELECT COUNT(*) FROM memberships WHERE site_id = ? AND group_id = ? AND status = 'member'`, db.SiteID, groupID).Scan(&n)
	return n
}

// ValidGroupRole reports whether a membership role is one of the three.
func ValidGroupRole(role string) bool {
	return role == GroupRoleAdmin || role == GroupRoleModerator || role == GroupRoleMember
}

// GroupAdmins returns the author ids running a group.
func (db *DB) GroupAdmins(groupID string) []string {
	return db.idList(`SELECT author_id FROM memberships WHERE site_id = ? AND group_id = ? AND role = 'admin' AND status = 'member' ORDER BY created, rowid`, db.SiteID, groupID)
}

// GroupModerators returns the author ids who moderate a group: its moderators
// and its admins (an admin can do everything a moderator can).
func (db *DB) GroupModerators(groupID string) []string {
	return db.idList(`SELECT author_id FROM memberships WHERE site_id = ? AND group_id = ? AND role IN ('admin', 'moderator') AND status = 'member' ORDER BY created, rowid`, db.SiteID, groupID)
}

// IsGroupMember reports whether a profile is a member (any role) of a group.
func (db *DB) IsGroupMember(groupID, authorID string) bool {
	_, status, ok := db.Membership(groupID, authorID)
	return ok && status == MembershipMember
}

// IsGroupAdmin reports whether a profile runs a group.
func (db *DB) IsGroupAdmin(groupID, authorID string) bool {
	role, status, ok := db.Membership(groupID, authorID)
	return ok && status == MembershipMember && role == GroupRoleAdmin
}

// IsGroupModerator reports whether a profile moderates a group (its moderators
// and its admins).
func (db *DB) IsGroupModerator(groupID, authorID string) bool {
	role, status, ok := db.Membership(groupID, authorID)
	return ok && status == MembershipMember && (role == GroupRoleAdmin || role == GroupRoleModerator)
}

// GroupIDsFor returns the ids of the groups a profile is a member of.
func (db *DB) GroupIDsFor(authorID string) []string {
	if authorID == "" {
		return []string{}
	}
	return db.idList(`SELECT group_id FROM memberships WHERE site_id = ? AND author_id = ? AND status = 'member' ORDER BY created, rowid`, db.SiteID, authorID)
}

// GroupsFor returns the slugs of the published groups a profile belongs to —
// {{ user.groups }}, so `{% members only if "board" in user.groups %}` works.
func (db *DB) GroupsFor(authorID string) []string {
	if authorID == "" {
		return []string{}
	}
	return db.idList(
		`SELECT p.slug FROM memberships m JOIN posts p ON p.id = m.group_id AND p.site_id = m.site_id
		 WHERE m.site_id = ? AND m.author_id = ? AND m.status = 'member' AND p.collection = ? AND p.status = 'published'
		 ORDER BY m.created, m.rowid`,
		db.SiteID, authorID, GroupsCollection,
	)
}

// CanSeeGroup applies a group's visibility to a viewer: public → everyone,
// members → anyone signed in, private → its members only. memberOf is the set of
// group ids the viewer belongs to (from GroupIDsFor), so a page's worth of checks
// costs one query.
func CanSeeGroup(record map[string]any, signedIn bool, memberOf map[string]bool) bool {
	visibility, _ := GroupSettings(record)
	switch visibility {
	case "public":
		return true
	case "members":
		return signedIn
	default:
		id, _ := record["id"].(string)
		return memberOf[id]
	}
}

// MemberOfSet is GroupIDsFor as a set.
func (db *DB) MemberOfSet(authorID string) map[string]bool {
	set := map[string]bool{}
	for _, id := range db.GroupIDsFor(authorID) {
		set[id] = true
	}
	return set
}

// AttachGroups sets record["group"] on every record — the published group its
// data.group names (a shallow copy of the group record), or nil — from a list of
// groups the caller already has, so nothing is queried per record.
func AttachGroups(records []map[string]any, groups []map[string]any) {
	bySlug := make(map[string]map[string]any, len(groups))
	for _, g := range groups {
		if slug, _ := g["slug"].(string); slug != "" {
			bySlug[slug] = g
		}
	}
	for _, r := range records {
		if g := bySlug[GroupSlugOf(r)]; g != nil {
			r["group"] = g
		} else {
			r["group"] = nil
		}
	}
}

// ListMemberships returns every membership row (for sync).
func (db *DB) ListMemberships() ([]map[string]any, error) {
	rows, err := db.Conn.Query(`SELECT id, group_id, author_id, role, status, created, updated FROM memberships WHERE site_id = ? ORDER BY created, rowid`, db.SiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, groupID, authorID, role, status, created, updated string
		if err := rows.Scan(&id, &groupID, &authorID, &role, &status, &created, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "group_id": groupID, "author_id": authorID, "role": role, "status": status, "created": created, "updated": updated})
	}
	return out, rows.Err()
}

// UpsertMembership writes a membership row by (group, author) (for sync).
func (db *DB) UpsertMembership(id, groupID, authorID, role, status, created string) error {
	if groupID == "" || authorID == "" {
		return nil
	}
	if id == "" {
		id = GenerateID()
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if created == "" {
		created = now
	}
	if !ValidGroupRole(role) {
		role = GroupRoleMember
	}
	if status != MembershipRequested && status != MembershipInvited {
		status = MembershipMember
	}
	_, err := db.Conn.Exec(
		`INSERT INTO memberships (id, site_id, group_id, author_id, role, status, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(site_id, group_id, author_id) DO UPDATE SET role = excluded.role, status = excluded.status, updated = excluded.updated`,
		id, db.SiteID, groupID, authorID, role, status, created, now,
	)
	return err
}

// DeleteMembershipsForGroup drops every row of a group (the group was deleted).
func (db *DB) DeleteMembershipsForGroup(groupID string) error {
	_, err := db.Conn.Exec(`DELETE FROM memberships WHERE site_id = ? AND group_id = ?`, db.SiteID, groupID)
	return err
}

// DeleteMembershipsForAuthor drops a profile's memberships everywhere.
func (db *DB) DeleteMembershipsForAuthor(authorID string) error {
	_, err := db.Conn.Exec(`DELETE FROM memberships WHERE site_id = ? AND author_id = ?`, db.SiteID, authorID)
	return err
}
