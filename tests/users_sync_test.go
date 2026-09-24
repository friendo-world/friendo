// Covers `friendo push/pull --users`: an account's password must survive a sync
// (the pull side used to omit the hash and the push side then blanked it), and
// a blank hash from an older CLI never erases a real one.
package tests

import (
	"strings"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

func TestUsersSyncKeepsPasswords(t *testing.T) {
	src := t.TempDir()
	dbA, err := data.Open(src)
	if err != nil {
		t.Fatalf("opening db A: %v", err)
	}
	defer dbA.Close()
	a := newAPIClient(t, dbA, src)
	st, out := a.do("POST", "/setup", map[string]any{"email": "owner@test.com", "name": "Owner", "password": "password12345"})
	a.want(201, st, out, "setup A")

	// Pull carries the real bcrypt hash.
	st, out = a.do("GET", "/pull/users", nil)
	a.want(200, st, out, "pull users")
	users := out["users"].([]any)
	var pulled map[string]any
	for _, u := range users {
		if m := u.(map[string]any); m["email"] == "owner@test.com" {
			pulled = m
		}
	}
	if pulled == nil {
		t.Fatalf("owner missing from pull: %v", users)
	}
	hash, _ := pulled["password_hash"].(string)
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("pulled password_hash should be a bcrypt hash, got %q", hash)
	}

	// Push it into a second site: the same password signs in there.
	dst := t.TempDir()
	dbB, err := data.Open(dst)
	if err != nil {
		t.Fatalf("opening db B: %v", err)
	}
	defer dbB.Close()
	b := newAPIClient(t, dbB, dst)
	st, out = b.do("POST", "/setup", map[string]any{"email": "admin@b.test", "name": "B", "password": "password12345"})
	b.want(201, st, out, "setup B")
	st, out = b.do("POST", "/push/users", map[string]any{"users": []any{pulled}, "authors": out["authors"]})
	b.want(200, st, out, "push users to B")
	if _, err := dbB.AuthenticateUser("owner@test.com", "password12345"); err != nil {
		t.Fatalf("synced password should work on B: %v", err)
	}

	// An older CLI pushes a blank hash: the existing one is preserved.
	blank := map[string]any{}
	for k, v := range pulled {
		blank[k] = v
	}
	blank["password_hash"] = ""
	st, out = b.do("POST", "/push/users", map[string]any{"users": []any{blank}})
	b.want(200, st, out, "push blank hash")
	if _, err := dbB.AuthenticateUser("owner@test.com", "password12345"); err != nil {
		t.Fatalf("a blank pushed hash must not erase the real one: %v", err)
	}
}
