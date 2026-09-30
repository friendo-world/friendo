package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
)

// upload sends one small PNG to POST /files for a post's field.
func (s *visitorSite) upload(cookie, postID, field string) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("post_id", postID)
	mw.WriteField("field", field)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	part.Write([]byte("\x89PNG not really"))
	mw.Close()
	req := httptest.NewRequest("POST", "/_/api/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "203.0.113.9:4444"
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

// A member adds an image to their own pending post only when the site allows
// it; the server writes it into the post.
func TestMemberUploads(t *testing.T) {
	s := newVisitorSite(t, "members_can_post = true")
	_, member := s.signIn("m@test.com", "")
	rec, _ := s.do("POST", "/collections/stories/posts", member, `{"title":"Mine"}`)
	post := decode(t, rec)["post"].(map[string]any)["id"].(string)

	if rec := s.upload(member, post, "cover"); rec.Code != http.StatusForbidden {
		t.Fatalf("upload with the switch off = %d, want 403", rec.Code)
	}
	s.db.SetSetting(settingMembersCanUpload, "true")
	rec = s.upload(member, post, "cover")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	f := decode(t, rec)["file"].(map[string]any)
	if f["attached"] != true {
		t.Fatalf("file = %v", f)
	}
	if queue, _ := s.db.ListRecordsByStatus("pending"); len(queue) != 1 || queue[0]["image"] != f["url"] {
		t.Fatalf("the review queue should show the image: %v", queue)
	}
	got, _ := s.db.GetRecordByID(post)
	if got["fields"].(map[string]any)["cover"] != f["url"] {
		t.Fatalf("the post should point at the image: %v", got["fields"])
	}
	if rec := s.upload(member, post, "../cover"); rec.Code != http.StatusBadRequest {
		t.Fatalf("odd field name = %d, want 400", rec.Code)
	}
	other, _ := s.db.CreateRecord("stories", "theirs", "Theirs", "", "pending", "")
	if rec := s.upload(member, other, "cover"); rec.Code != http.StatusNotFound {
		t.Fatalf("someone else's post = %d, want 404", rec.Code)
	}
	for i := 1; i < maxSubmissionFiles; i++ {
		s.upload(member, post, "gallery")
	}
	if rec := s.upload(member, post, "gallery"); rec.Code != http.StatusBadRequest {
		t.Fatalf("over the per-post cap = %d, want 400", rec.Code)
	}
	s.db.SetRecordStatus(post, "published")
	if rec := s.upload(member, post, "cover"); rec.Code != http.StatusConflict {
		t.Fatalf("after approval = %d, want 409", rec.Code)
	}
}

// A visitor's upload has its own switch, and taking the post back removes it.
func TestVisitorUploads(t *testing.T) {
	s := newVisitorSite(t, "visitors_can_post = true")
	rec, visitor := s.do("POST", "/collections/tips/posts", "", `{"title":"Tip"}`)
	post := decode(t, rec)["post"].(map[string]any)["id"].(string)

	if rec := s.upload(visitor, post, "photo"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("visitor upload with the switch off = %d, want 401", rec.Code)
	}
	s.db.SetSetting(settingVisitorsCanUpload, "true")
	rec = s.upload(visitor, post, "photo")
	if rec.Code != http.StatusCreated {
		t.Fatalf("visitor upload = %d %s", rec.Code, rec.Body.String())
	}
	key := decode(t, rec)["file"].(map[string]any)["r2_key"].(string)
	if _, err := os.Stat(filepath.Join(s.dir, key)); err != nil {
		t.Fatalf("stored file: %v", err)
	}
	if rec := s.upload("", post, "photo"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rec.Code)
	}
	if rec, _ := s.do("POST", "/posts/"+post+"/withdraw", visitor, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("withdraw = %d", rec.Code)
	}
	if files, _ := s.db.ListFiles("post", post); len(files) != 0 {
		t.Fatalf("files should go with the post: %v", files)
	}
	if _, err := os.Stat(filepath.Join(s.dir, key)); !os.IsNotExist(err) {
		t.Fatalf("stored bytes should be removed: %v", err)
	}
}

// A contributor uploads to, and removes files from, only their own posts; an
// editor may on any post.
func TestUploadOwnership(t *testing.T) {
	s := newVisitorSite(t, "")
	session := func(email, role string) (string, string) {
		u, _ := s.db.CreateMember(email, "", role)
		token, _ := s.db.CreateSession(u.ID, "", "")
		return token, s.db.DefaultAuthorID(u.ID)
	}
	alice, aliceAuthor := session("a@test.com", "contributor")
	bob, _ := session("b@test.com", "contributor")
	editor, _ := session("e@test.com", "editor")
	post, _ := s.db.CreateRecord("blog", "x", "X", "", "published", aliceAuthor)

	rec := s.upload(alice, post, "cover")
	if rec.Code != http.StatusCreated || decode(t, rec)["file"].(map[string]any)["attached"] != nil {
		t.Fatalf("own post = %d %s", rec.Code, rec.Body.String())
	}
	file := decode(t, rec)["file"].(map[string]any)["id"].(string)
	if rec := s.upload(bob, post, "cover"); rec.Code != http.StatusForbidden {
		t.Fatalf("someone else's post = %d, want 403", rec.Code)
	}
	if rec := s.upload(bob, "no-such-post", "cover"); rec.Code != http.StatusNotFound {
		t.Fatalf("a missing post = %d, want 404", rec.Code)
	}
	if rec, _ := s.do("DELETE", "/files/"+file, bob, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("deleting someone else's file = %d, want 403", rec.Code)
	}
	if rec := s.upload(editor, post, "gallery"); rec.Code != http.StatusCreated {
		t.Fatalf("editor = %d, want 201", rec.Code)
	}
	if rec, _ := s.do("DELETE", "/files/"+file, alice, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting own file = %d", rec.Code)
	}
}
