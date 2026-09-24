// Migration 0012 seeds access.password_login ON for a site whose people already
// have passwords (so an upgrade never locks anyone out) and leaves a code-only
// site untouched.
package tests

import (
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

func reopenWithout0012(t *testing.T, dir string, db *data.DB) *data.DB {
	t.Helper()
	if _, err := db.Conn.Exec(`DELETE FROM site_settings WHERE key = 'access.password_login'`); err != nil {
		t.Fatalf("clearing setting: %v", err)
	}
	if _, err := db.Conn.Exec(`DELETE FROM schema_migrations WHERE id = 12`); err != nil {
		t.Fatalf("unrecording migration: %v", err)
	}
	db.Close()
	re, err := data.Open(dir)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	return re
}

func TestPasswordLoginMigrationSeedsUpgradedSites(t *testing.T) {
	dir := t.TempDir()
	db, err := data.Open(dir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	if _, err := db.CreateUser("owner@test.com", "Owner", "password12345", "owner"); err != nil {
		t.Fatalf("creating owner: %v", err)
	}
	db = reopenWithout0012(t, dir, db)
	defer db.Close()
	if !db.GetBoolSetting("access.password_login", false) {
		t.Fatal("a site with a password-holding user should have password_login seeded on")
	}
}

func TestPasswordLoginMigrationLeavesCodeOnlySitesAlone(t *testing.T) {
	dir := t.TempDir()
	db, err := data.Open(dir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	if _, err := db.CreateMember("owner@test.com", "Owner", "owner"); err != nil {
		t.Fatalf("creating owner: %v", err)
	}
	db = reopenWithout0012(t, dir, db)
	defer db.Close()
	if db.GetSetting("access.password_login", "") != "" {
		t.Fatal("a code-only site should not get password_login seeded")
	}
}
