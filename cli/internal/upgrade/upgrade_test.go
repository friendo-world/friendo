package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves a GitHub-shaped release (latest tag, archive, checksums.txt)
// whose archive holds binary. corrupt breaks the checksum.
func fakeRelease(t *testing.T, tag string, binary []byte, corrupt bool) {
	t.Helper()
	archive := buildArchive(t, binary)
	name := assetName(tag)
	sum := sha256.Sum256(archive)
	if corrupt {
		sum[0] ^= 0xff
	}
	sums := fmt.Sprintf("%s  %s\n%s  friendo_other.tar.gz\n", hex.EncodeToString(sum[:]), name, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": %q}`, tag)
	})
	mux.HandleFunc("/dl/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/dl/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldAPI, oldDL := apiBase, downloadBase
	apiBase, downloadBase = srv.URL+"/repo", srv.URL+"/dl"
	t.Cleanup(func() { apiBase, downloadBase = oldAPI, oldDL })
}

func buildArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("friendo.exe")
		w.Write(binary)
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "friendo", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	tw.Write(binary)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func fakeExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "friendo")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestUpgradeReplacesBinary(t *testing.T) {
	fakeRelease(t, "v0.7.0", []byte("new binary"), false)
	exe := fakeExe(t)

	if err := Run(Options{Current: "0.6.0", Exe: exe}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new binary" {
		t.Fatalf("binary = %q, want the new one", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %v, want 0755", fi.Mode().Perm())
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".friendo-upgrade-*"))
	if len(leftovers) > 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestUpgradeRejectsBadChecksum(t *testing.T) {
	fakeRelease(t, "v0.7.0", []byte("new binary"), true)
	exe := fakeExe(t)

	err := Run(Options{Current: "0.6.0", Exe: exe})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed to %q after a failed check", got)
	}
}

func TestUpgradeLeavesBinaryAlone(t *testing.T) {
	cases := map[string]Options{
		"already latest":    {Current: "v0.7.0"},
		"newer than latest": {Current: "0.8.0"},
		"check only":        {Current: "0.6.0", CheckOnly: true},
		"go install":        {Current: "v0.6.0", GoInstall: true},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			fakeRelease(t, "v0.7.0", []byte("new binary"), false)
			o.Exe = fakeExe(t)
			if err := Run(o); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(o.Exe); string(got) != "old binary" {
				t.Fatalf("binary changed to %q", got)
			}
		})
	}
}

func TestUpgradeRefusesSourceBuild(t *testing.T) {
	if err := Run(Options{Current: "dev", Exe: fakeExe(t)}); err == nil {
		t.Fatal("want an error for a build from source")
	}
}

func TestUpgradeBacksUpDatabase(t *testing.T) {
	fakeRelease(t, "v0.7.0", []byte("new binary"), false)
	site := t.TempDir()
	os.MkdirAll(filepath.Join(site, "data"), 0o755)
	db, err := sql.Open("sqlite", filepath.Join(site, "data", "friendo.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`CREATE TABLE posts (title TEXT)`)
	db.Exec(`INSERT INTO posts VALUES ('hello')`)
	defer db.Close() // stays open, like a running `friendo serve`

	if err := Run(Options{Current: "0.6.0", Exe: fakeExe(t), SiteDir: site}); err != nil {
		t.Fatal(err)
	}

	backup, err := sql.Open("sqlite", filepath.Join(site, "data", "friendo-before-v0.6.0.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var title string
	if err := backup.QueryRow(`SELECT title FROM posts`).Scan(&title); err != nil || title != "hello" {
		t.Fatalf("backup read = %q, %v", title, err)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.7.0", "v0.6.0", 1},
		{"v0.6.0", "v0.6.0", 0},
		{"v0.6.0", "v0.10.0", -1},
		{"v1.0.0", "v0.99.9", 1},
		{"v0.7.0-rc1", "v0.7.0", -1},
		{"v0.7.0", "v0.7.0-rc1", 1},
		{"v0.6.1-0.20260929-abcdef", "v0.6.0", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
