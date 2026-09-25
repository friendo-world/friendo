package calendar

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Feed serves a site's /calendar.ics and /calendar.json.
type Feed struct {
	DB       *data.DB
	SiteName string
	// Visible says whether a collection may appear (a members-only collection
	// must not leak through a public feed). nil = every collection.
	Visible func(collection string) bool
	// Permalink resolves a post's public path so entries can link back.
	Permalink data.PermalinkFunc
}

// filterFrom reads ?collection= and ?record= .
func filterFrom(r *http.Request) Filter {
	q := r.URL.Query()
	return Filter{Collection: strings.TrimSpace(q.Get("collection")), RecordID: strings.TrimSpace(q.Get("record"))}
}

// baseURL is the site's own origin as the request saw it (behind a proxy,
// X-Forwarded-Proto says whether the outside is HTTPS).
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	if r.Host == "" {
		return ""
	}
	return scheme + "://" + r.Host
}

// ServeICS answers /calendar.ics. Query: collection=, record=, and with record=
// also occurrence=<RFC 3339 start> for a single instance of a recurring event.
func (f Feed) ServeICS(w http.ResponseWriter, r *http.Request) {
	entries, err := Collect(f.DB, f.Visible, f.Permalink, filterFrom(r))
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	occurrence := strings.TrimSpace(r.URL.Query().Get("occurrence"))
	tag := ETag(entries, f.SiteName+"|ics|"+occurrence+"|"+baseURL(r))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "public, max-age=900")
	w.Header().Set("Vary", "Accept-Encoding")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	WriteICS(w, entries, ICSOptions{
		SiteName:   f.SiteName,
		SiteZone:   f.DB.Location,
		BaseURL:    baseURL(r),
		Occurrence: occurrence,
	})
}

// ServeJSON answers /calendar.json: occurrences in [from, to) (default: the next
// twelve months, at most two years), soonest first.
func (f Feed) ServeJSON(w http.ResponseWriter, r *http.Request) {
	entries, err := Collect(f.DB, f.Visible, f.Permalink, filterFrom(r))
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	from, to := Window(r.URL.Query().Get("from"), r.URL.Query().Get("to"), f.DB.Location)
	tag := ETag(entries, f.SiteName+"|json|"+from.Format(time.RFC3339)+"|"+to.Format(time.RFC3339)+"|"+baseURL(r))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Vary", "Accept-Encoding")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"site":     f.SiteName,
		"timezone": zoneName(f.DB.Location),
		"from":     from.Format(time.RFC3339),
		"to":       to.Format(time.RFC3339),
		"events":   Occurrences(entries, from, to, baseURL(r)),
	})
}

func zoneName(loc *time.Location) string {
	if loc == nil {
		return "UTC"
	}
	return loc.String()
}

// Window reads a from/to pair (RFC 3339 or YYYY-MM-DD, in loc): defaults to
// now → +12 months, and never spans more than two years.
func Window(fromStr, toStr string, loc *time.Location) (from, to time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	parse := func(s string) (time.Time, bool) {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.In(loc), true
		}
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return t, true
		}
		if t, err := time.ParseInLocation("2006-01", s, loc); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	from, ok := parse(fromStr)
	if !ok {
		from = now
	}
	to, ok = parse(toStr)
	if !ok || !to.After(from) {
		to = from.AddDate(0, 12, 0)
	}
	if to.After(from.AddDate(2, 0, 0)) {
		to = from.AddDate(2, 0, 0)
	}
	return from, to
}
