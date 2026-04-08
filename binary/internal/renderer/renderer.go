package renderer

import (
	"fmt"
	"time"

	"github.com/flosch/pongo2/v6"
)

func init() {
	RegisterFilters()
}

// RegisterFilters registers all custom Pongo2 filters.
func RegisterFilters() {
	pongo2.RegisterFilter("asset_url", filterAssetURL)
	pongo2.RegisterFilter("date", filterDate)
	pongo2.RegisterFilter("resize", filterResize)
}

// asset_url: resolves a file reference to a public URL.
// Both locally and on the edge, assets are served from /public/.
func filterAssetURL(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	val := in.String()
	if val == "" {
		return pongo2.AsValue(""), nil
	}
	return pongo2.AsValue(fmt.Sprintf("/public/%s", val)), nil
}

// date: formats a date string using Go's time format.
// Usage: {{ post.created|date:"Jan 2, 2006" }}
func filterDate(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	layout := param.String()
	if layout == "" {
		layout = "2006-01-02"
	}

	raw := in.String()
	if raw == "" {
		return pongo2.AsValue(""), nil
	}

	// Try common formats
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05.000Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return pongo2.AsValue(t.Format(layout)), nil
		}
	}

	return nil, &pongo2.Error{
		Sender:    "filter:date",
		OrigError: fmt.Errorf("cannot parse date %q", raw),
	}
}

// resize: stub filter for image resizing.
// In local dev mode, this returns the value unchanged.
// Full implementation deferred to Phase 2 (see PHASE_1.md open questions).
func filterResize(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	return in, nil
}
