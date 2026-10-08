package data

import (
	"os"
	"testing"
)

// The database must run in WAL mode with a busy timeout, so readers don't fail
// while another request writes. modernc's driver only honors pragmas written
// as _pragma=name(value); the mattn-style keys it used to be given were ignored.
func TestOpenPragmas(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/friendo.toml", []byte("[site]\nname = \"t\"\n"), 0o644)
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.Conn.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
	var wait int
	if err := db.Conn.QueryRow(`PRAGMA busy_timeout`).Scan(&wait); err != nil || wait != 5000 {
		t.Fatalf("busy_timeout = %d, %v; want 5000", wait, err)
	}
}
