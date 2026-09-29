package calendar

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// ErrBadToken is a ?token= nobody holds (reset, or mistyped): the feed answers
// 401 rather than quietly becoming the public one.
var ErrBadToken = errors.New("that calendar link isn't valid any more — ask the site for a new one")

// Feed serves a site's /calendar.ics and /calendar.json.
type Feed struct {
	DB       *data.DB
	SiteName string
	// View says who's asking and what they may see: from ?token= (a member's
	// private feed) or the session cookie, else a visitor's public view. nil =
	// everything is public. It returns the viewer's id ("" for a visitor).
	View func(r *http.Request) (View, string, error)
	// Permalink resolves a post's public path so entries can link back.
	Permalink data.PermalinkFunc
}

// filterFrom reads ?collection=, ?record= and ?group= .
func filterFrom(r *http.Request) Filter {
	q := r.URL.Query()
	return Filter{Collection: strings.TrimSpace(q.Get("collection")), RecordID: strings.TrimSpace(q.Get("post")), Group: strings.TrimSpace(q.Get("group"))}
}

// view resolves the request's viewer, answering 401 itself for a bad token.
func (f Feed) view(w http.ResponseWriter, r *http.Request) (View, string, bool) {
	if f.View == nil {
		return View{}, "", true
	}
	v, id, err := f.View(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return View{}, "", false
	}
	return v, id, true
}

// cacheHeaders: a visitor's feed is public and cacheable; a member's is theirs.
func cacheHeaders(w http.ResponseWriter, viewerID string, maxAge int) {
	scope := "public"
	if viewerID != "" {
		scope = "private"
	}
	w.Header().Set("Cache-Control", scope+", max-age="+itoa(maxAge))
	w.Header().Set("Vary", "Cookie, Accept-Encoding")
}

func itoa(n int) string { return strconv.Itoa(n) }

// BaseURL is the site's own origin as the request saw it (behind a proxy,
// X-Forwarded-Proto says whether the outside is HTTPS).
func BaseURL(r *http.Request) string { return baseURL(r) }

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
// also date=<RFC 3339 start> for a single date of a repeating event.
func (f Feed) ServeICS(w http.ResponseWriter, r *http.Request) {
	view, viewerID, ok := f.view(w, r)
	if !ok {
		return
	}
	entries, err := Collect(f.DB, view, f.Permalink, filterFrom(r))
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	occurrence := strings.TrimSpace(r.URL.Query().Get("date"))
	tag := ETag(entries, f.SiteName+"|ics|"+occurrence+"|"+baseURL(r)+"|u="+viewerID+"|mine="+r.URL.Query().Get("mine"))
	w.Header().Set("ETag", tag)
	cacheHeaders(w, viewerID, 900)
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
	view, viewerID, ok := f.view(w, r)
	if !ok {
		return
	}
	entries, err := Collect(f.DB, view, f.Permalink, filterFrom(r))
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	from, to := Window(r.URL.Query().Get("from"), r.URL.Query().Get("to"), f.DB.Location)
	tag := ETag(entries, f.SiteName+"|json|"+from.Format(time.RFC3339)+"|"+to.Format(time.RFC3339)+"|"+baseURL(r)+"|u="+viewerID+"|mine="+r.URL.Query().Get("mine"))
	w.Header().Set("ETag", tag)
	cacheHeaders(w, viewerID, 300)
	var extra url.Values
	if view.Token != "" {
		extra = url.Values{"token": {view.Token}}
	}
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
		"events":   Occurrences(entries, from, to, baseURL(r), extra),
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
