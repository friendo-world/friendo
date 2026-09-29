package data

import (
	"errors"
	"testing"
)

func TestFollowsToggleFriendsAndCounts(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mk := func(email, name string) string {
		u, err := db.CreateMember(email, name, "member")
		if err != nil {
			t.Fatal(err)
		}
		return db.DefaultAuthorID(u.ID)
	}
	pat, sam, kim := mk("pat@t.com", "Pat"), mk("sam@t.com", "Sam"), mk("kim@t.com", "Kim")

	if _, err := db.ToggleFollow(pat, pat); !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("self follow: err = %v, want ErrSelfFollow", err)
	}
	on, err := db.ToggleFollow(pat, sam)
	if err != nil || !on {
		t.Fatalf("pat follows sam: %v %v", on, err)
	}
	if on, _ := db.ToggleFollow(pat, kim); !on {
		t.Fatal("pat follows kim")
	}
	if !db.IsFollowing(pat, sam) || db.IsFollowing(sam, pat) {
		t.Fatal("IsFollowing is one-way")
	}
	if got := db.Following(pat); len(got) != 2 || got[0] != sam || got[1] != kim {
		t.Fatalf("Following(pat) = %v", got)
	}
	if got := db.Followers(sam); len(got) != 1 || got[0] != pat {
		t.Fatalf("Followers(sam) = %v", got)
	}
	if got := db.Friends(pat); len(got) != 0 {
		t.Fatalf("no friends yet, got %v", got)
	}
	// Sam follows back: they're friends; Kim doesn't.
	db.ToggleFollow(sam, pat)
	if got := db.Friends(pat); len(got) != 1 || got[0] != sam {
		t.Fatalf("Friends(pat) = %v, want [sam]", got)
	}
	if got := db.Friends(sam); len(got) != 1 || got[0] != pat {
		t.Fatalf("Friends(sam) = %v, want [pat]", got)
	}
	followers, following := db.FollowCounts(pat)
	if followers != 1 || following != 2 {
		t.Fatalf("FollowCounts(pat) = %d, %d", followers, following)
	}
	profiles, _ := db.FollowerProfiles(pat)
	if len(profiles) != 1 || profiles[0]["slug"] != "sam" {
		t.Fatalf("FollowerProfiles(pat) = %v", profiles)
	}
	if _, has := profiles[0]["email"]; has {
		t.Fatal("a follower profile must never carry the email")
	}

	// Toggling again unfollows; unfollowing twice is fine.
	if on, _ := db.ToggleFollow(pat, kim); on {
		t.Fatal("second toggle should unfollow")
	}
	if err := db.Unfollow(pat, kim); err != nil {
		t.Fatal(err)
	}
	if got := db.Following(pat); len(got) != 1 {
		t.Fatalf("Following(pat) after unfollow = %v", got)
	}

	// Sync shape round-trips and ignores duplicates / self pairs.
	list, err := db.ListFollows()
	if err != nil || len(list) != 2 {
		t.Fatalf("ListFollows = %v %v", list, err)
	}
	for _, f := range list {
		if err := db.UpsertFollow(f["id"].(string), f["follower_id"].(string), f["followee_id"].(string), f["created"].(string)); err != nil {
			t.Fatal(err)
		}
	}
	db.UpsertFollow("", kim, kim, "")
	if list, _ := db.ListFollows(); len(list) != 2 {
		t.Fatalf("upsert should not duplicate: %v", list)
	}

	// Deleting a profile drops its follows on both sides.
	if err := db.DeleteFollowsForAuthor(sam); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListFollows(); len(list) != 0 {
		t.Fatalf("follows after deleting sam = %v", list)
	}
	if got := db.Following(""); got == nil || len(got) != 0 {
		t.Fatalf("Following(\"\") must be an empty list, got %v", got)
	}
}
