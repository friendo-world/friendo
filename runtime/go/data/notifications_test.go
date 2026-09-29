package data

import (
	"database/sql"
	"testing"
)

func TestNotifications(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pat, _ := db.CreateMember("pat@t.com", "Pat", "member")
	sam, _ := db.CreateMember("sam@t.com", "Sam", "member")
	patA, samA := db.DefaultAuthorID(pat.ID), db.DefaultAuthorID(sam.ID)
	patB, _ := db.CreateProfile(pat.ID, "Pat Two", "")

	// Sam follows Pat: one row; doing it again doesn't add another.
	db.Notify(patA, NotifyFollow, "author", patA, samA)
	db.Notify(patA, NotifyFollow, "author", patA, samA)
	// Pat's other profile following Pat: skipped (same account).
	db.Notify(patA, NotifyFollow, "author", patA, patB["id"].(string))
	// An empty recipient is a no-op.
	db.Notify("", NotifyFollow, "author", "", samA)
	if n := db.UnreadCount(pat.ID); n != 1 {
		t.Fatalf("UnreadCount(pat) = %d, want 1", n)
	}
	if n := db.UnreadCount(sam.ID); n != 0 {
		t.Fatalf("UnreadCount(sam) = %d, want 0", n)
	}

	// The inbox is the union over profiles.
	db.Notify(patB["id"].(string), NotifyComment, "comment", "c1", samA)
	list, err := db.ListNotifications(pat.ID, false, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListNotifications = %v %v", list, err)
	}
	if list[0].Kind != NotifyComment || list[0].Actor == nil || list[0].Actor["slug"] != "sam" {
		t.Fatalf("newest first with actor: %+v", list[0])
	}
	if _, has := list[0].Actor["email"]; has {
		t.Fatal("an actor profile must never carry the email")
	}

	// Mark one read; the other account can't touch it.
	if err := db.MarkRead(sam.ID, list[0].ID); err != sql.ErrNoRows {
		t.Fatalf("marking someone else's notification: %v, want ErrNoRows", err)
	}
	if err := db.MarkRead(pat.ID, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if n := db.UnreadCount(pat.ID); n != 1 {
		t.Fatalf("UnreadCount after one read = %d", n)
	}
	unread, _ := db.ListNotifications(pat.ID, true, 0)
	if len(unread) != 1 || unread[0].Kind != NotifyFollow {
		t.Fatalf("unread only = %+v", unread)
	}
	db.MarkAllRead(pat.ID)
	if n := db.UnreadCount(pat.ID); n != 0 {
		t.Fatalf("UnreadCount after read-all = %d", n)
	}
	if !db.NotificationsOn() {
		t.Fatal("notifications should be on while follows is")
	}
	db.SetSetting(FeatureSetting("follows"), "false")
	db.SetSetting(FeatureSetting("groups"), "false")
	if db.NotificationsOn() {
		t.Fatal("notifications should be off when follows and groups are")
	}
}
