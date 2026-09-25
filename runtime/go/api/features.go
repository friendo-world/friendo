package api

import (
	"net/http"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// featureGate answers for a feature that's switched off: 403 with `off: true`,
// which the SDK's tags read as "render nothing". Reads and writes alike — an
// off feature is off for visitors too.
func featureGate(db *data.DB, feature string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !db.FeatureOn(feature) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"` + data.FeatureLabel[feature] + ` are turned off for this site","off":true}`))
			return
		}
		next(w, r)
	}
}

// handleFeatures is public: which features this site has on, for the SDK and
// the admin to lay themselves out.
func handleFeatures(db *data.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]any{"features": featuresMap(db)})
	}
}

func featuresMap(db *data.DB) map[string]bool {
	out := map[string]bool{}
	for _, f := range data.Features {
		out[f] = db.FeatureOn(f)
	}
	return out
}

// --- Default collections ---

// settingDefaultCollections holds which of the built-in collections a site with
// no declared [content] types shows, as a comma-separated list.
const settingDefaultCollections = "content.default_collections"

// defaultCollectionsFor reads the site's chosen defaults (all three unless set).
func defaultCollectionsFor(db *data.DB) []string {
	raw := db.GetSetting(settingDefaultCollections, "")
	if raw == "" {
		return defaultCollections
	}
	var out []string
	for _, n := range strings.Split(raw, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// normalizeDefaultCollections keeps only the built-in names, in their usual order.
func normalizeDefaultCollections(names []string) []string {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	out := []string{}
	for _, n := range defaultCollections {
		if want[n] {
			out = append(out, n)
		}
	}
	return out
}
