package api

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Visitors: someone who hasn't signed in can react, vote or RSVP when the site
// turns that on (one visitors_can_* setting per action, all off by default).
// Their first action creates a visitor account and a long-lived session (see
// data/visitors.go); signing in later carries what they did onto their account.
//
// authFunc never returns a visitor, so every existing check stays closed to
// them. Only two things here ever see one:
//   - viewerFunc, for public reads that say what's yours ("you reacted").
//   - visitorsMay, which wraps the few write routes a visitor may use.

// Visitor settings: what someone who hasn't signed in may do.
const (
	settingVisitorsCanReact = "visitors_can_react"
	settingVisitorsCanVote  = "visitors_can_vote"
	settingVisitorsCanRSVP  = "visitors_can_rsvp"
	// A visitor's comments and posts always wait for review.
	settingVisitorsCanComment = "visitors_can_comment"
	settingVisitorsCanPost    = "visitors_can_post"
)

// visitorActions maps each action a visitor may take to its setting.
var visitorActions = map[string]string{
	"react":   settingVisitorsCanReact,
	"vote":    settingVisitorsCanVote,
	"rsvp":    settingVisitorsCanRSVP,
	"comment": settingVisitorsCanComment,
	"post":    settingVisitorsCanPost,
	"upload":  settingVisitorsCanUpload, // images on their own pending post (uploads.go)
}

// Visitor rate limits, keyed by network address since a visitor has no email:
// how many new visitors one address may start per hour, and how many actions
// it may take per window.
const (
	visitorNewLimit     = 20
	visitorNewWindow    = time.Hour
	visitorActionLimit  = 30
	visitorActionWindow = 5 * time.Minute
)

type visitorKey struct{}

// authFn is how a handler learns who's asking (nil = nobody signed in).
type authFn = func(*http.Request) *data.User

// visitorsCan reports whether the site lets visitors take an action.
func visitorsCan(db *data.DB, action string) bool {
	key, ok := visitorActions[action]
	return ok && db.GetBoolSetting(key, false)
}

// sessionVisitor returns the visitor this request's session belongs to, or nil.
func sessionVisitor(r *http.Request, db *data.DB) (*data.User, string) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, ""
	}
	u, err := db.ValidateSession(cookie.Value)
	if err != nil || !u.IsVisitor() {
		return nil, ""
	}
	return u, cookie.Value
}

// viewerFunc is authFunc that also recognizes a visitor who already has a
// session. It never creates one. For public reads only: the answer tells a
// visitor what's theirs, and a visitor holds no capabilities.
func viewerFunc(db *data.DB, authFunc func(*http.Request) *data.User) func(*http.Request) *data.User {
	return func(r *http.Request) *data.User {
		if u := authFunc(r); u != nil {
			return u
		}
		u, _ := sessionVisitor(r, db)
		return u
	}
}

// visitorsMay lets a visitor through to a write route when the site allows
// the action: it finds (or starts) their visitor account, renews its session,
// applies the per-address limits, and hands the handler an auth function that
// returns the visitor. Members pass straight through, and with the setting
// off nothing changes — the handler answers 401 as it always has.
func visitorsMay(db *data.DB, authFunc authFn, action string, h func(authFn) http.HandlerFunc) http.HandlerFunc {
	actor := func(r *http.Request) *data.User {
		if u := authFunc(r); u != nil {
			return u
		}
		v, _ := r.Context().Value(visitorKey{}).(*data.User)
		return v
	}
	next := h(actor)
	return func(w http.ResponseWriter, r *http.Request) {
		if authFunc(r) != nil || !visitorsCan(db, action) {
			next(w, r)
			return
		}
		ip := clientIP(r)
		if !db.RateLimitAllow("visitor-"+action+":"+ip, visitorActionLimit, visitorActionWindow) {
			jsonError(w, "too many changes — slow down", http.StatusTooManyRequests)
			return
		}
		v, token := sessionVisitor(r, db)
		if v != nil {
			db.ExtendSession(token, data.VisitorSessionTTL)
		} else {
			if !db.RateLimitAllow("visitor-new:"+ip, visitorNewLimit, visitorNewWindow) {
				jsonError(w, "too many new visitors from your network — sign in instead", http.StatusTooManyRequests)
				return
			}
			var err error
			if v, err = db.CreateVisitor(); err != nil {
				jsonError(w, "could not start a visitor session", http.StatusInternalServerError)
				return
			}
			if token, err = db.CreateVisitorSession(v.ID, ip, r.UserAgent()); err != nil {
				jsonError(w, "could not start a visitor session", http.StatusInternalServerError)
				return
			}
		}
		setCookieFor(w, r, token, data.VisitorSessionTTL)
		next(w, r.WithContext(context.WithValue(r.Context(), visitorKey{}, v)))
	}
}

// carryVisitor runs when someone signs in. If this browser was a visitor, what
// they did moves onto the account they signed in to. A failure here is logged
// and never blocks the sign-in.
func carryVisitor(r *http.Request, db *data.DB, user *data.User) {
	v, _ := sessionVisitor(r, db)
	if v == nil || v.ID == user.ID {
		return
	}
	if err := db.MergeVisitor(v.ID, user.ID); err != nil {
		log.Printf("carrying a visitor's activity to %s: %v", user.ID, err)
	}
}

// handleVisitor tells the SDK what a visitor may do here and, when this
// browser is already a visitor, the name they gave. A signed-in member gets
// visitor: null — they're a member.
func handleVisitor(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		can := map[string]bool{}
		for action := range visitorActions {
			can[action] = visitorsCan(db, action)
		}
		var visitor any
		if authFunc(r) == nil {
			if v, _ := sessionVisitor(r, db); v != nil {
				visitor = map[string]any{"name": db.VisitorName(v.ID)}
			}
		}
		jsonResponse(w, map[string]any{"visitor": visitor, "can": can})
	}
}

// setVisitorName applies the optional name a visitor sent with a comment or
// post, answering 400 (and false) when it can't be used.
func setVisitorName(w http.ResponseWriter, db *data.DB, user *data.User, name string) bool {
	if !user.IsVisitor() || strings.TrimSpace(name) == "" {
		return true
	}
	if err := db.SetVisitorName(user.ID, name); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// trapped reports whether a visitor filled in the hidden "trap" field the SDK
// adds to its forms. People never see it; form-filling bots fill everything.
// The bot is told it worked (202, nothing saved), so it has nothing to learn.
func trapped(w http.ResponseWriter, user *data.User, trap string) bool {
	if !user.IsVisitor() || strings.TrimSpace(trap) == "" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"ok":true}` + "\n"))
	return true
}

// VisitorContext is {{ visitor }} in templates: nil for a signed-in member,
// otherwise what a visitor may do here and, when this browser already is one,
// their name — so a page can say "Sign in to keep this" or show a composer.
func VisitorContext(r *http.Request, db *data.DB, signedIn bool) map[string]any {
	if signedIn {
		return nil
	}
	can := map[string]any{}
	for action := range visitorActions {
		can[action] = visitorsCan(db, action)
	}
	out := map[string]any{"can": can, "known": false, "name": ""}
	if v, _ := sessionVisitor(r, db); v != nil {
		out["known"] = true
		out["name"] = db.VisitorName(v.ID)
	}
	return out
}

// clientIP is the address a request came from, for visitor rate limits.
// Behind a proxy (the peer is on this machine or a private network) it
// trusts, in order, Cloudflare's CF-Connecting-IP and then the address the
// proxy appended last to X-Forwarded-For; anything to the left of that is
// whatever the client claimed. Directly connected, it's the peer. IPv6
// addresses are cut to their /64, the block one household is usually given.
func clientIP(r *http.Request) string {
	peer := r.RemoteAddr
	if h, _, err := net.SplitHostPort(peer); err == nil {
		peer = h
	}
	ip := net.ParseIP(strings.Trim(peer, "[]"))
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if cf := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); cf != nil {
			ip = cf
		} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if last := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); last != nil {
				ip = last
			}
		}
	}
	if ip == nil {
		return peer
	}
	if ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return ip.String()
}
