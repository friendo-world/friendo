package renderer

import "github.com/flosch/pongo2/v6"

// Social filters (v0.6). A pongo2 filter sees only its input and its argument —
// not the viewer, not the database — so these read what the page context already
// attached: record.author_id on every record, and user.following on the viewer.

// in_group: the records filed under a group (`group: board` in their front
// matter), by slug. A plain string compare on data.group, so a value that names
// no group is just data. Usage: {% for p in collections.blog|in_group:"board" %}
// or, on a group's page, |in_group:record.slug.
func filterInGroup(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	list, ok := in.Interface().([]map[string]any)
	if !ok {
		return in, nil
	}
	slug := param.String()
	out := []map[string]any{}
	if slug == "" {
		return pongo2.AsValue(out), nil
	}
	for _, r := range list {
		d, _ := r["fields"].(map[string]any)
		if g, _ := d["group"].(string); g == slug {
			out = append(out, r)
		}
	}
	return pongo2.AsValue(out), nil
}

// by_following: the records written by people the viewer follows — a "people I
// follow" feed. The argument is the viewer: {% for p in collections.blog|by_following:user %}.
// A visitor (no user) gets an empty list.
func filterByFollowing(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	list, ok := in.Interface().([]map[string]any)
	out := []map[string]any{}
	if !ok {
		return in, nil
	}
	user, _ := param.Interface().(map[string]any)
	following, _ := user["following"].([]string)
	if len(following) == 0 {
		return pongo2.AsValue(out), nil
	}
	set := make(map[string]bool, len(following))
	for _, id := range following {
		set[id] = true
	}
	for _, r := range list {
		if id, _ := r["author_id"].(string); set[id] {
			out = append(out, r)
		}
	}
	return pongo2.AsValue(out), nil
}
