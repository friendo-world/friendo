package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/storage"
)

// Uploads from people who can't edit posts. Contributors and up upload to any
// post and then save the post with the image's address (handleUploadFile). A
// plain member or a visitor can't edit posts, so when the site allows it
// (members_can_upload, visitors_can_upload) they may upload only to their own
// post while it waits for review — right after submitting it from a
// <friendo-form> — and the server writes the image into the post itself.

const (
	// Members can add images to what they post; they wait for review with it.
	settingMembersCanUpload = "members_can_upload"
	// Visitors can too (visitors_can_post must be on for them to post at all).
	settingVisitorsCanUpload = "visitors_can_upload"
)

// maxSubmissionFiles caps how many files one submitted post may carry.
const maxSubmissionFiles = 10

// fieldName is what a submitted file's field may be called: a plain name, so it
// can go straight into the post's fields.
var fieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// handleFiles routes an upload: one carrying a drop-box post's upload_key to
// handleDropBoxUpload, contributors and up to the full handler, and members or
// visitors (when allowed) to handleSubmissionUpload.
func handleFiles(db *data.DB, siteDir string, store storage.Backend, authFunc, viewer authFn) http.HandlerFunc {
	full := handleUploadFile(db, siteDir, store, authFunc)
	return func(w http.ResponseWriter, r *http.Request) {
		// Parsed once here; the handlers below reuse the parsed form.
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			jsonError(w, "expected a multipart form upload", http.StatusBadRequest)
			return
		}
		if r.FormValue("upload_key") != "" {
			handleDropBoxUpload(w, r, db, siteDir, store, dropBoxNames(db))
			return
		}
		user := viewer(r)
		switch {
		case user == nil:
			jsonError(w, "unauthorized", http.StatusUnauthorized)
		case user.Can(data.CapContentCreate):
			full(w, r)
		case user.IsVisitor() && visitorsCan(db, "upload"),
			!user.IsVisitor() && db.GetBoolSetting(settingMembersCanUpload, false):
			handleSubmissionUpload(w, r, db, siteDir, store, user)
		case user.IsVisitor():
			jsonError(w, "unauthorized", http.StatusUnauthorized)
		default:
			jsonError(w, "forbidden", http.StatusForbidden)
		}
	}
}

// handleSubmissionUpload stores one image for a member's or visitor's own
// pending post and writes its address into the post's fields.<field>.
func handleSubmissionUpload(w http.ResponseWriter, r *http.Request, db *data.DB, siteDir string, store storage.Backend, user *data.User) {
	postID := r.FormValue("post_id")
	if !db.UserOwnsPost(user.ID, postID) {
		jsonError(w, "post not found", http.StatusNotFound)
		return
	}
	if !submissionCanTakeFile(w, db, postID) {
		return
	}
	if !db.RateLimitAllow("upload:"+user.ID, commentRateLimit, commentRateWindow) ||
		(user.IsVisitor() && !db.RateLimitAllow("visitor-upload:"+clientIP(r), commentRateLimit, commentRateWindow)) {
		jsonError(w, "you're uploading too fast — slow down", http.StatusTooManyRequests)
		return
	}
	storeAndAttach(w, r, db, siteDir, store, postID)
}

// handleDropBoxUpload stores one image for a drop-box post. Nobody owns such a
// post, so the proof is the upload_key its creation handed back — good for one
// post, for UploadKeyTTL, and tied to no one.
func handleDropBoxUpload(w http.ResponseWriter, r *http.Request, db *data.DB, siteDir string, store storage.Backend, boxes map[string]bool) {
	postID := r.FormValue("post_id")
	rec, err := db.GetRecordByID(postID)
	if err != nil || !boxes[fmt.Sprint(rec["collection"])] || !db.UploadKeyValid(postID, r.FormValue("upload_key")) {
		jsonError(w, "that upload key isn't good for this post (it may have expired)", http.StatusForbidden)
		return
	}
	if !submissionCanTakeFile(w, db, postID) {
		return
	}
	if !db.RateLimitAllow("dropbox-upload:"+clientIP(r), commentRateLimit, commentRateWindow) {
		jsonError(w, "you're uploading too fast — slow down", http.StatusTooManyRequests)
		return
	}
	storeAndAttach(w, r, db, siteDir, store, postID)
}

// submissionCanTakeFile checks what every submitted-post upload shares: the post
// still waits for review, the field is a plain name, and it isn't full. It
// answers the error itself and returns false when not.
func submissionCanTakeFile(w http.ResponseWriter, db *data.DB, postID string) bool {
	rec, err := db.GetRecordByID(postID)
	if err != nil {
		jsonError(w, "post not found", http.StatusNotFound)
		return false
	}
	if rec["status"] != "pending" {
		jsonError(w, "images can only be added while the post waits for review", http.StatusConflict)
		return false
	}
	if files, _ := db.ListFiles("post", postID); len(files) >= maxSubmissionFiles {
		jsonError(w, fmt.Sprintf("a post can carry %d files at most", maxSubmissionFiles), http.StatusBadRequest)
		return false
	}
	return true
}

// storeAndAttach saves the request's image and writes its address into the
// post's fields.<field>, answering the file (attached: true) or the error.
func storeAndAttach(w http.ResponseWriter, r *http.Request, db *data.DB, siteDir string, store storage.Backend, postID string) {
	field := r.FormValue("field")
	if !fieldName.MatchString(field) {
		jsonError(w, "field must be a plain name", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "a file field is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	mime := header.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		jsonError(w, "only image uploads are supported", http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = extForMime(mime)
	}
	id := data.GenerateID()
	key := "assets/uploads/" + id + ext
	size, err := writeAsset(r.Context(), siteDir, store, key, io.LimitReader(file, 10<<20), mime)
	if err != nil {
		jsonError(w, fmt.Sprintf("storage error: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := db.CreateFileWithID(id, "post", postID, field, key, mime, size); err != nil {
		removeAsset(r.Context(), siteDir, store, key)
		jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
		return
	}
	out, _ := db.GetFile(id)
	if err := db.SetRecordField(postID, field, out["url"]); err != nil {
		jsonError(w, fmt.Sprintf("attach error: %v", err), http.StatusInternalServerError)
		return
	}
	// attached: the post already points at the image — nothing left to save.
	out["attached"] = true
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, map[string]any{"file": out})
}

// removeRecordFiles deletes every file on a post, rows and stored bytes
// (best-effort), for a post taken back before review.
func removeRecordFiles(ctx context.Context, db *data.DB, siteDir string, store storage.Backend, postID string) {
	files, _ := db.ListFiles("post", postID)
	for _, f := range files {
		id, _ := f["id"].(string)
		if key, err := db.DeleteFile(id); err == nil {
			removeAsset(ctx, siteDir, store, key)
		}
	}
}
