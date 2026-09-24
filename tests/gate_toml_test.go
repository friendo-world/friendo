// Covers friendo.toml's [access] table: a whole path can be members-only without
// a tag on each page, it applies before routing (so a 404 under the prefix also
// shows the sign-in page), and a page's own tag can still ask for more.
package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/server"
	"net/http/httptest"
)

func TestAccessRulesInToml(t *testing.T) {
	t.Setenv("FRIENDO_OTP_ECHO", "1")
	siteDir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(siteDir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("friendo.toml", "[site]\nname = \"Gated\"\n\n[access]\nmembers_only = [\"/members/*\", \"/downloads\"]\neditors_only = [\"/newsroom/*\"]\n")
	write("pages/index.html", "home")
	write("pages/members/index.html", "members home")
	write("pages/members/deep.html", "deep")
	write("pages/members/staff.html", "{% editors only %}staff inside members")
	write("pages/downloads.html", "downloads")
	write("pages/newsroom/index.html", "newsroom")
	write("pages/404.html", "not found")
	write("pages/login.html", "login {{ gate.required }} {{ gate.reason }} {{ gate.path }}")

	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler, err := server.BuildSiteHandler(siteDir, db, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	visitor := newBrowser(t, srv.URL)
	status, body, _ := visitor.get("/")
	expect(t, "visitor home", status, 200, body, "home")
	status, body, _ = visitor.get("/members")
	expect(t, "visitor prefix root", status, 401, body, "login member signin /members")
	status, body, _ = visitor.get("/members/deep")
	expect(t, "visitor under prefix", status, 401, body, "login member signin /members/deep")
	status, body, _ = visitor.get("/members/nothing")
	expect(t, "404 under prefix hides itself", status, 401, body, "login member signin")
	status, body, _ = visitor.get("/downloads")
	expect(t, "exact path", status, 401, body, "login member signin /downloads")
	status, body, _ = visitor.get("/downloadsx")
	expect(t, "exact path is exact", status, 404, body, "not found")
	status, body, _ = visitor.get("/newsroom")
	expect(t, "editors prefix", status, 401, body, "login editor signin")

	member := newBrowser(t, srv.URL)
	member.signInMember("m@test.com")
	status, body, _ = member.get("/members/deep")
	expect(t, "member under prefix", status, 200, body, "deep")
	status, body, _ = member.get("/newsroom")
	expect(t, "member in editors prefix", status, 403, body, "login editor role /newsroom")
	status, body, _ = member.get("/members/staff")
	expect(t, "page tag raises the bar", status, 403, body, "login editor role /members/staff")
}
