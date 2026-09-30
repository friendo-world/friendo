package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Drop boxes: a form in a template that says drop-box makes its collection
// take posts from anyone — signed in or not — and keep no one's name, not even
// for moderators:
//
//	<friendo-form collection="tips" drop-box>
//
// The server reads that from the template files when the site loads (see
// server/template_scan.go). It makes the whole collection a drop box, whichever
// form a post comes from. It's for a tip line, anonymous feedback, a suggestion box. A post there is
// made with no author, always waits for review, and leaves nothing that ties it
// to a person: no visitor account is started and a signed-in member's account
// isn't recorded. (The per-address rate limit counts requests for a few minutes;
// it's never linked to the post.) With no author, nobody can take a post back;
// images follow with the one-time upload_key the post's creation hands back.

// dropBoxOr sends a post to a drop-box collection to handleDropBoxPost and any
// other post on to next.
func dropBoxOr(db *data.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dropBoxNames(db)[chi.URLParam(r, "collection")] {
			handleDropBoxPost(w, r, db, chi.URLParam(r, "collection"))
			return
		}
		next(w, r)
	}
}

// dropBoxNames is the set of drop boxes right now, as the templates last said.
func dropBoxNames(db *data.DB) map[string]bool {
	out := map[string]bool{}
	for _, name := range db.PageDropBoxes() {
		out[name] = true
	}
	return out
}

// handleDismissClosedDropBox clears the admin's "no longer a drop box" notice
// for a collection once someone has seen it.
func handleDismissClosedDropBox(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db.DismissClosedDropBox(chi.URLParam(r, "collection"))
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDropBoxPost(w http.ResponseWriter, r *http.Request, db *data.DB, collection string) {
	var in recordInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	// The trap: people never see it, bots fill it. Look done, save nothing.
	if strings.TrimSpace(in.Trap) != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"ok":true}` + "\n"))
		return
	}
	hasData := len(in.Data) > 0 && string(in.Data) != "null" && string(in.Data) != "{}"
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Body) == "" && !hasData {
		jsonError(w, "there's nothing to send", http.StatusBadRequest)
		return
	}
	if !db.RateLimitAllow("dropbox:"+clientIP(r), commentRateLimit, commentRateWindow) {
		jsonError(w, "you're sending too fast — slow down", http.StatusTooManyRequests)
		return
	}
	// No author, whoever sent it, and it always waits for review.
	id, err := db.CreateRecord(collection, in.Slug, in.Title, in.Body, "pending", "")
	if err != nil {
		jsonError(w, fmt.Sprintf("create error: %v", err), http.StatusInternalServerError)
		return
	}
	if hasData {
		cleaned := liftWhen(db, id, in.Data)
		db.SetRecordData(id, string(cleaned))
		autoGeotag(db, id, in.Title, cleaned)
	}
	record, _ := db.GetRecordByID(id)
	attachWhen(db, record)
	record["drop_box"] = true
	// The one-time key that lets images follow (uploads.go); nobody owns the post.
	if key, err := db.CreateUploadKey(id); err == nil {
		record["upload_key"] = key
	}
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, map[string]any{"post": record})
}
