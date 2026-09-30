package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// newDropBoxSite is a site whose templates (as the server would have scanned
// them) mark "tips" drop-box.
func newDropBoxSite(t *testing.T) *visitorSite {
	s := newVisitorSite(t, "")
	s.db.SetPageDropBoxes([]string{"tips"})
	return s
}

func countVisitors(s *visitorSite) int {
	var n int
	s.db.Conn.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'visitor'`).Scan(&n)
	return n
}

// Anyone can post to a drop box, no name is kept, and nothing ties the post to
// whoever sent it.
func TestDropBoxKeepsNoName(t *testing.T) {
	s := newDropBoxSite(t)

	rec, cookie := s.do("POST", "/collections/tips/posts", "", `{"title":"A tip","status":"published","author_name":"Robin"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("visitor = %d %s", rec.Code, rec.Body.String())
	}
	p := decode(t, rec)["post"].(map[string]any)
	if p["status"] != "pending" || p["author_id"] != "" || p["drop_box"] != true {
		t.Fatalf("post = %v", p)
	}
	if cookie != "" || countVisitors(s) != 0 {
		t.Fatal("a drop box must not start a visitor")
	}

	_, member := s.signIn("m@test.com", "")
	rec, _ = s.do("POST", "/collections/tips/posts", member, `{"body":"from a member"}`)
	mp := decode(t, rec)["post"].(map[string]any)
	if mp["author_id"] != "" {
		t.Fatalf("a member's name must not be kept either: %v", mp)
	}
	queue, _ := s.db.ListRecordsByStatus("pending")
	found := false
	for _, q := range queue {
		if q["id"] == mp["id"] {
			found = q["excerpt"] == "from a member" && q["author_name"] == ""
		}
	}
	if !found {
		t.Fatalf("review queue should show the body and no name: %v", queue)
	}
	if rec, _ := s.do("POST", "/posts/"+mp["id"].(string)+"/withdraw", member, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("withdrawing a nameless post = %d, want 404", rec.Code)
	}

	editor, _ := s.db.CreateMember("e@test.com", "E", "editor")
	token, _ := s.db.CreateSession(editor.ID, "", "")
	rec, _ = s.do("POST", "/collections/tips/posts", token, `{"title":"staff","status":"published"}`)
	if ep := decode(t, rec)["post"].(map[string]any); ep["author_id"] != "" || ep["status"] != "pending" {
		t.Fatalf("an editor's drop-box post = %v", ep)
	}
}

func TestDropBoxRules(t *testing.T) {
	s := newDropBoxSite(t)
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty = %d, want 400", rec.Code)
	}
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"spam","trap":"x"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("trapped = %d, want 202", rec.Code)
	}
	if list, _ := s.db.ListRecordsByStatus("pending"); len(list) != 0 {
		t.Fatalf("nothing should be saved: %v", list)
	}
	// Only declared boxes; the others work as before.
	if rec, _ := s.do("POST", "/collections/blog/posts", "", `{"title":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a visitor posting to blog = %d, want 401", rec.Code)
	}
	rec, _ := s.do("GET", "/visitor", "", "")
	if boxes := decode(t, rec)["drop_boxes"].([]any); len(boxes) != 1 || boxes[0] != "tips" {
		t.Fatalf("drop_boxes = %v", boxes)
	}
	for i := 0; i < commentRateLimit; i++ {
		s.do("POST", "/collections/tips/posts", "", `{"title":"x"}`)
	}
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"x"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over the limit = %d, want 429", rec.Code)
	}
}

// drop_box in friendo.toml isn't read: a drop box is said on the form.
func TestDropBoxNotFromToml(t *testing.T) {
	s := newVisitorSite(t, "\n[content]\ncollections = [\"tips\"]\n\n[content.tips]\ndrop_box = true\n")
	if rec, _ := s.do("POST", "/collections/tips/posts", "", `{"title":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a toml-only drop box = %d, want 401", rec.Code)
	}
	types, _ := loadContentTypes(s.dir)
	if out, _ := SuggestedContentToml(s.db, types); strings.Contains(out, "drop_box") {
		t.Fatalf("the suggested block shouldn't carry drop_box: %s", out)
	}
}

// When a collection stops being a drop box, the admin hears about it until
// someone dismisses it.
func TestClosedDropBoxNotice(t *testing.T) {
	s := newDropBoxSite(t)
	s.do("POST", "/collections/tips/posts", "", `{"title":"x"}`)
	s.db.SetPageDropBoxes(nil)
	owner, _ := s.db.CreateMember("o@test.com", "O", "owner")
	token, _ := s.db.CreateSession(owner.ID, "", "")
	closedFlag := func() any {
		rec, _ := s.do("GET", "/collections", token, "")
		for _, c := range decode(t, rec)["collections"].([]any) {
			if m := c.(map[string]any); m["name"] == "tips" {
				return m["drop_box_closed"]
			}
		}
		return nil
	}
	if closedFlag() != true {
		t.Fatal("tips should be flagged as no longer a drop box")
	}
	if rec, _ := s.do("DELETE", "/collections/tips/drop-box-notice", token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss = %d", rec.Code)
	}
	if closedFlag() != nil {
		t.Fatal("the notice should be gone")
	}
}

// uploadWithKey sends one PNG to a drop-box post with its upload key.
func (s *visitorSite) uploadWithKey(postID, key string) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("post_id", postID)
	mw.WriteField("field", "photo")
	mw.WriteField("upload_key", key)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	part.Write([]byte("\x89PNG not really"))
	mw.Close()
	req := httptest.NewRequest("POST", "/_/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "203.0.113.9:4444"
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

// Images follow a drop-box post with the one-time key its creation returned —
// that post only, while it waits, for a limited time.
func TestDropBoxUploads(t *testing.T) {
	s := newDropBoxSite(t)
	send := func() (string, string) {
		rec, _ := s.do("POST", "/collections/tips/posts", "", `{"body":"look at this"}`)
		p := decode(t, rec)["post"].(map[string]any)
		key, _ := p["upload_key"].(string)
		if key == "" {
			t.Fatalf("no upload key: %v", p)
		}
		return p["id"].(string), key
	}
	post, key := send()
	other, otherKey := send()

	rec := s.uploadWithKey(post, key)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	f := decode(t, rec)["file"].(map[string]any)
	got, _ := s.db.GetRecordByID(post)
	if f["attached"] != true || got["fields"].(map[string]any)["photo"] != f["url"] || got["author_id"] != "" {
		t.Fatalf("post = %v, file = %v", got, f)
	}
	if rec := s.uploadWithKey(post, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong key = %d, want 403", rec.Code)
	}
	if rec := s.uploadWithKey(post, otherKey); rec.Code != http.StatusForbidden {
		t.Fatalf("another post's key = %d, want 403", rec.Code)
	}
	// A key only ever comes from a drop box: a blog post can't be reached with one.
	blog, _ := s.db.CreateRecord("blog", "b", "B", "", "pending", "")
	if rec := s.uploadWithKey(blog, key); rec.Code != http.StatusForbidden {
		t.Fatalf("a key on a non-drop-box post = %d, want 403", rec.Code)
	}
	s.db.Conn.Exec(`UPDATE upload_keys SET expires_at = '2000-01-01T00:00:00Z' WHERE post_id = ?`, other)
	if rec := s.uploadWithKey(other, otherKey); rec.Code != http.StatusForbidden {
		t.Fatalf("expired key = %d, want 403", rec.Code)
	}
	s.db.SetRecordStatus(post, "published")
	if rec := s.uploadWithKey(post, key); rec.Code != http.StatusConflict {
		t.Fatalf("after approval = %d, want 409", rec.Code)
	}
}
