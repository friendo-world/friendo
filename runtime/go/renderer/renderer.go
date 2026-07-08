package renderer

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md is a shared goldmark instance (GitHub-flavored markdown: tables, strikethrough,
// autolinks, task lists).
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

func init() {
	RegisterFilters()
}

// RegisterFilters registers all custom Pongo2 filters.
func RegisterFilters() {
	pongo2.RegisterFilter("asset_url", filterAssetURL)
	pongo2.RegisterFilter("date", filterDate)
	pongo2.RegisterFilter("resize", filterResize)
	pongo2.RegisterFilter("markdown", filterMarkdown)
	pongo2.RegisterFilter("sort_by", filterSortBy)
}

// sort_by: stably sorts a list of records by a (possibly dotted) key, numerically
// when both values are numbers, else lexicographically.
// Usage: {% for d in collections.docs|sort_by:"data.weight" %}
func filterSortBy(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	list, ok := in.Interface().([]map[string]any)
	if !ok {
		return in, nil
	}
	key := param.String()
	sorted := make([]map[string]any, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool {
		av, bv := nestedValue(sorted[i], key), nestedValue(sorted[j], key)
		af, aok := toFloat(av)
		bf, bok := toFloat(bv)
		if aok && bok {
			return af < bf
		}
		return fmt.Sprintf("%v", av) < fmt.Sprintf("%v", bv)
	})
	return pongo2.AsValue(sorted), nil
}

// nestedValue walks a dotted key path (e.g. "data.weight") through nested maps.
func nestedValue(m map[string]any, key string) any {
	var cur any = m
	for _, part := range strings.Split(key, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[part]
	}
	return cur
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}

// markdown: renders a markdown string to HTML. The result is marked safe so
// {{ post.body|markdown }} outputs rendered HTML rather than escaped text.
// Usage: {{ post.body|markdown }}
func filterMarkdown(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	raw := in.String()
	if raw == "" {
		return pongo2.AsSafeValue(""), nil
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(raw), &buf); err != nil {
		return nil, &pongo2.Error{Sender: "filter:markdown", OrigError: err}
	}
	return pongo2.AsSafeValue(buf.String()), nil
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
