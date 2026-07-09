// Package sdk embeds and serves friendo.js — the community Web Components client.
// The file is generated from sdk/friendo.js by sdk/scripts/bundle.mjs (npm run
// sdk); the edge runtime ships the byte-identical copy via runtime/edge/sdk-bundle.js.
package sdk

import (
	_ "embed"
	"net/http"
)

//go:embed friendo.js
var script string

// Handler serves friendo.js as JavaScript. Public — the SDK loads on visitor
// pages and talks to the runtime-agnostic /_/api endpoints.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Write([]byte(script))
	}
}
