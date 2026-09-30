// Package upgrade replaces the running friendo binary with a release from GitHub —
// the Go twin of scripts/install.sh. It picks the same archive the installer does
// (GoReleaser's name_template), checks it against the release's checksums.txt, and
// swaps the executable in place. The site database updates itself the next time
// friendo opens it, so the binary is the only thing to change.
package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Where releases live. Variables so tests can point them at a local server.
var (
	apiBase      = "https://api.github.com/repos/friendo-world/friendo"
	downloadBase = "https://github.com/friendo-world/friendo/releases/download"
	httpClient   = &http.Client{Timeout: 5 * time.Minute}
)

// Options for Run.
type Options struct {
	Current   string // the running version, e.g. "0.6.0" or "v0.6.0"
	GoInstall bool   // installed with `go install` — let Go replace it instead
	Target    string // a release tag to install; "" means the latest
	CheckOnly bool   // report whether a newer release exists, change nothing
	SiteDir   string // back up <SiteDir>/data/friendo.db first, if it exists
	Exe       string // the binary to replace; "" means the running one
}

// Run does the whole upgrade and prints what it's doing.
func Run(o Options) error {
	current := normalize(o.Current)
	if current == "vdev" {
		return errors.New("this friendo was built from source, so there's no release to compare it with.\n" +
			"Rebuild it, or install a release: https://friendo.world/docs/upgrade")
	}

	target := normalize(o.Target)
	if target == "" {
		tag, err := latestTag()
		if err != nil {
			return err
		}
		target = tag
	}

	cmp := compareVersions(target, current)
	if o.CheckOnly {
		if cmp > 0 {
			fmt.Printf("friendo %s is available (you have %s). Run `friendo upgrade` to install it.\n", target, current)
		} else {
			fmt.Printf("friendo %s is the latest release.\n", current)
		}
		return nil
	}
	if cmp == 0 {
		fmt.Printf("friendo %s is already installed.\n", current)
		return nil
	}
	if cmp < 0 && o.Target == "" {
		fmt.Printf("friendo %s is newer than the latest release (%s). Nothing to do.\n", current, target)
		return nil
	}

	if o.GoInstall {
		fmt.Println("This friendo was installed with `go install`, so upgrade it the same way:")
		fmt.Printf("  go install github.com/friendo-world/friendo/cli/cmd/friendo@%s\n", target)
		return nil
	}

	if cmp < 0 {
		fmt.Printf("Going back from %s to %s. A newer friendo may have changed your database in ways\n", current, target)
		fmt.Println("an older one can't read; if the site won't start, restore the backup below.")
	}

	if o.SiteDir != "" {
		if err := backupDatabase(o.SiteDir, current); err != nil {
			return err
		}
	}

	exe := o.Exe
	if exe == "" {
		p, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding the friendo binary: %w", err)
		}
		exe = p
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}

	fmt.Printf("Downloading friendo %s (%s/%s)...\n", target, runtime.GOOS, runtime.GOARCH)
	bin, err := download(target)
	if err != nil {
		return err
	}
	if err := replaceBinary(exe, bin); err != nil {
		return err
	}

	fmt.Printf("Upgraded friendo %s → %s (%s)\n", current, target, exe)
	fmt.Println("Restart `friendo serve` to use it. Your database updates itself when it starts.")
	return nil
}

// latestTag asks GitHub for the newest published release.
func latestTag() (string, error) {
	resp, err := httpClient.Get(apiBase + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("checking for the latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checking for the latest release: GitHub answered %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName == "" {
		return "", errors.New("checking for the latest release: couldn't read GitHub's answer")
	}
	return normalize(rel.TagName), nil
}

// assetName matches GoReleaser's name_template and scripts/install.sh.
func assetName(tag string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("friendo_%s_%s_%s.%s", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH, ext)
}

// download fetches the archive for this platform, checks its SHA-256 against the
// release's checksums.txt, and returns the friendo binary inside it.
func download(tag string) ([]byte, error) {
	name := assetName(tag)
	archive, err := fetch(fmt.Sprintf("%s/%s/%s", downloadBase, tag, name))
	if err != nil {
		return nil, err
	}
	sums, err := fetch(fmt.Sprintf("%s/%s/checksums.txt", downloadBase, tag))
	if err != nil {
		return nil, err
	}
	want, ok := checksumFor(sums, name)
	if !ok {
		return nil, fmt.Errorf("%s isn't listed in the release's checksums.txt", name)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s doesn't match its checksum — the download may be damaged; try again", name)
	}
	return extractBinary(archive, strings.HasSuffix(name, ".zip"))
}

func fetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("downloading %s: not found — is that a real release, with a build for %s/%s?", url, runtime.GOOS, runtime.GOARCH)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// checksumFor finds name's line in a GoReleaser checksums.txt ("<sha256>  <file>").
func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extractBinary pulls the friendo executable out of a release archive.
func extractBinary(archive []byte, isZip bool) ([]byte, error) {
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("opening the download: %w", err)
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == "friendo.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("the download has no friendo.exe in it")
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("opening the download: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("the download has no friendo binary in it")
		}
		if err != nil {
			return nil, fmt.Errorf("opening the download: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "friendo" {
			return io.ReadAll(tr)
		}
	}
}

// replaceBinary writes the new binary next to the old one and renames it into
// place, so a failure part-way never leaves a half-written friendo behind.
// Windows won't overwrite a running .exe but will rename it, so the old one is
// moved aside to friendo.exe.old first.
func replaceBinary(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".friendo-upgrade-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("no permission to write to %s — run it again with sudo (sudo friendo upgrade)", dir)
		}
		return fmt.Errorf("writing the new binary: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return fmt.Errorf("writing the new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing the new binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("writing the new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("moving the old binary aside: %w", err)
		}
		if err := os.Rename(tmpPath, exe); err != nil {
			os.Rename(old, exe)
			return fmt.Errorf("putting the new binary in place: %w", err)
		}
		return nil
	}

	if err := os.Rename(tmpPath, exe); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("no permission to replace %s — run it again with sudo (sudo friendo upgrade)", exe)
		}
		return fmt.Errorf("putting the new binary in place: %w", err)
	}
	return nil
}

// backupDatabase copies data/friendo.db to data/friendo-before-<version>.db when
// the command runs inside a site folder. VACUUM INTO makes a consistent copy even
// while `friendo serve` has the database open.
func backupDatabase(siteDir, version string) error {
	db := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(db); err != nil {
		return nil // not in a site folder — nothing to back up
	}
	dest := filepath.Join(siteDir, "data", "friendo-before-"+version+".db")
	if _, err := os.Stat(dest); err == nil {
		fmt.Printf("Backup already there: %s\n", dest)
		return nil
	}
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		return fmt.Errorf("backing up the database: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Exec("VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("backing up the database: %w", err)
	}
	fmt.Printf("Backed up this site's database to %s\n", dest)
	return nil
}

// normalize turns "0.6.0" / "v0.6.0" into "v0.6.0"; "" stays "".
func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(v, "v")
}

// compareVersions compares vMAJOR.MINOR.PATCH tags, returning -1, 0 or 1. A
// pre-release suffix ("-rc1") sorts before the same version without one.
func compareVersions(a, b string) int {
	pa, sa := splitVersion(a)
	pb, sb := splitVersion(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case sa == sb:
		return 0
	case sa == "":
		return 1
	case sb == "":
		return -1
	case sa < sb:
		return -1
	default:
		return 1
	}
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, part := range strings.SplitN(core, ".", 3) {
		n[i], _ = strconv.Atoi(part)
	}
	return n, pre
}
