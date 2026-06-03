package renderer

import (
	"fmt"
	"strings"
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

// resize: appends width/height hints to an image URL as query parameters.
// Usage: {{ post.image|asset_url|resize:"300x200" }} -> /public/img.jpg?w=300&h=200
// The spec is "W", "WxH", or "xH". These are consumed by an image-resizing CDN
// (e.g. Cloudflare Image Resizing); the built-in static server ignores them and
// serves the original, so this degrades gracefully.
func filterResize(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	url := in.String()
	query := resizeQuery(param.String())
	if url == "" || query == "" {
		return in, nil
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return pongo2.AsValue(url + sep + query), nil
}

// resizeQuery turns a resize spec ("300", "300x200", "x200") into a query string.
func resizeQuery(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	parts := strings.SplitN(spec, "x", 2)
	var params []string
	if w := strings.TrimSpace(parts[0]); w != "" {
		params = append(params, "w="+w)
	}
	if len(parts) == 2 {
		if h := strings.TrimSpace(parts[1]); h != "" {
			params = append(params, "h="+h)
		}
	}
	return strings.Join(params, "&")
}
