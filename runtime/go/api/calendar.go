package api

import (
	"fmt"
	"net/http"

	"github.com/friendo-world/friendo/runtime/go/calendar"
	"github.com/friendo-world/friendo/runtime/go/data"
)

// A member's private calendar link.
//
//	GET  /_/api/me/calendar        {token, ics, json, webcal, google}
//	POST /_/api/me/calendar/reset  a new token; the old link stops working
//
// The feed itself is /calendar.ics?token=… (and /calendar.json?token=…): what
// this member may see — members-only collections they pass, their groups'
// events — plus ?mine=1 for just the events they answered or were invited to,
// and ?group= for one group's. The token is the secret; treat the link as one.

func calendarLinks(r *http.Request, token string) map[string]any {
	out := map[string]any{"token": token}
	for k, v := range calendar.PrivateLinks(calendar.BaseURL(r), token) {
		out[k] = v
	}
	return out
}

func handleMyCalendar(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if _, ok := requireAuthor(db, w, user); !ok {
			return
		}
		token, err := db.CalendarToken(user.ID)
		if err != nil {
			jsonError(w, fmt.Sprintf("calendar error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		jsonResponse(w, calendarLinks(r, token))
	}
}

func handleResetMyCalendar(db *data.DB, authFunc func(*http.Request) *data.User) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := authFunc(r)
		if _, ok := requireAuthor(db, w, user); !ok {
			return
		}
		token, err := db.ResetCalendarToken(user.ID)
		if err != nil {
			jsonError(w, fmt.Sprintf("calendar error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		jsonResponse(w, calendarLinks(r, token))
	}
}
