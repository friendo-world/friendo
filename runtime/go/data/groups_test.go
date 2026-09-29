package data

import "testing"

func TestGroupsMembershipRules(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mk := func(email, name string) string {
		u, _ := db.CreateMember(email, name, "member")
		return db.DefaultAuthorID(u.ID)
	}
	pat, sam := mk("pat@t.com", "Pat"), mk("sam@t.com", "Sam")
	board, _ := db.CreateRecord(GroupsCollection, "board", "The Board", "", "published", pat)
	db.SetRecordData(board, `{"visibility":"private","join":"request"}`)
	club, _ := db.CreateRecord(GroupsCollection, "club", "Book club", "", "published", pat)
	draft, _ := db.CreateRecord(GroupsCollection, "secret", "Draft", "", "draft", pat)

	if _, err := db.GroupBySlug("board"); err != nil {
		t.Fatalf("GroupBySlug: %v", err)
	}
	if _, err := db.GroupBySlug("secret"); err == nil {
		t.Fatal("an unpublished group doesn't exist")
	}
	if _, err := db.GroupByID(draft); err == nil {
		t.Fatal("GroupByID on a draft")
	}
	g, _ := db.GroupBySlug("board")
	if v, j := GroupSettings(g); v != "private" || j != "request" {
		t.Fatalf("settings = %s %s", v, j)
	}
	c, _ := db.GroupBySlug("club")
	if v, j := GroupSettings(c); v != "public" || j != "open" {
		t.Fatalf("defaults = %s %s", v, j)
	}

	// Membership rows and roles.
	db.SetMembership(board, pat, GroupRoleAdmin, MembershipMember)
	db.SetMembership(board, sam, GroupRoleMember, MembershipRequested)
	if !db.IsGroupAdmin(board, pat) || !db.IsGroupModerator(board, pat) || db.IsGroupMember(board, sam) {
		t.Fatal("admin / requested standing wrong")
	}
	if n := db.MemberCount(board); n != 1 {
		t.Fatalf("MemberCount = %d", n)
	}
	if got := db.GroupsFor(pat); len(got) != 1 || got[0] != "board" {
		t.Fatalf("GroupsFor(pat) = %v", got)
	}
	if got := db.GroupsFor(sam); len(got) != 0 {
		t.Fatalf("GroupsFor(sam) with a pending request = %v", got)
	}
	req, _ := db.ListMembers(board, MembershipRequested)
	if len(req) != 1 || req[0]["slug"] != "sam" || req[0]["status"] != MembershipRequested {
		t.Fatalf("requested = %v", req)
	}
	db.SetMembership(board, sam, GroupRoleMember, MembershipMember)
	members, _ := db.ListMembers(board, "")
	if len(members) != 2 || members[0]["slug"] != "pat" || members[0]["role"] != GroupRoleAdmin || members[1]["slug"] != "sam" {
		t.Fatalf("members (admins first) = %v", members)
	}
	if _, has := members[0]["email"]; has {
		t.Fatal("a member profile must never carry the email")
	}
	if mods := db.GroupModerators(board); len(mods) != 1 || mods[0] != pat {
		t.Fatalf("moderators (admins count) = %v", mods)
	}
	if admins := db.GroupAdmins(board); len(admins) != 1 || admins[0] != pat {
		t.Fatalf("admins = %v", admins)
	}

	// Visibility.
	set := db.MemberOfSet(sam)
	if !CanSeeGroup(g, true, set) || CanSeeGroup(g, true, map[string]bool{}) || CanSeeGroup(g, false, nil) {
		t.Fatal("private group visibility wrong")
	}
	if !CanSeeGroup(c, false, nil) {
		t.Fatal("public group should be visible signed out")
	}
	db.SetRecordData(club, `{"visibility":"members"}`)
	c, _ = db.GroupBySlug("club")
	if CanSeeGroup(c, false, nil) || !CanSeeGroup(c, true, nil) {
		t.Fatal("members visibility wrong")
	}

	// Filing by reference + AttachGroups.
	post, _ := db.CreateRecord("blog", "notes", "Notes", "", "published", pat)
	db.SetRecordData(post, `{"group":"board"}`)
	other, _ := db.CreateRecord("blog", "misc", "Misc", "", "published", pat)
	db.SetRecordData(other, `{"group":"Concepts"}`) // names no group: just data
	records, _ := db.QueryPublishedCollection("blog")
	groups, _ := db.QueryPublishedCollection(GroupsCollection)
	AttachGroups(records, groups)
	for _, r := range records {
		switch r["slug"] {
		case "notes":
			if gg, _ := r["group"].(map[string]any); gg == nil || gg["slug"] != "board" {
				t.Fatalf("notes.group = %v", r["group"])
			}
		case "misc":
			if r["group"] != nil {
				t.Fatalf("misc.group = %v, want nil", r["group"])
			}
		}
	}
	if GroupSlugOf(records[0]) == "" && GroupSlugOf(records[1]) == "" {
		t.Fatal("GroupSlugOf")
	}

	// Sync + cascade.
	list, _ := db.ListMemberships()
	if len(list) != 2 {
		t.Fatalf("ListMemberships = %v", list)
	}
	for _, m := range list {
		if err := db.UpsertMembership(m["id"].(string), m["group_id"].(string), m["author_id"].(string), m["role"].(string), m["status"].(string), m["created"].(string)); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := db.ListMemberships(); len(list) != 2 {
		t.Fatalf("upsert duplicated: %v", list)
	}
	db.RemoveMembership(board, sam)
	if db.IsGroupMember(board, sam) {
		t.Fatal("removed")
	}
	db.DeleteMembershipsForGroup(board)
	if n := db.MemberCount(board); n != 0 {
		t.Fatalf("after cascade = %d", n)
	}
	if db.MembersCanStartGroups() {
		t.Fatal("members_can_start_groups should default off")
	}
}
