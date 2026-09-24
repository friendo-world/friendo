package renderer

import "testing"

func TestFindGate(t *testing.T) {
	cases := map[string]string{
		"plain": "",
		"{% extends \"x\" %}\n{% members only %}\n": "{% members only %}",
		"{%- editors only -%}":                      "{%- editors only -%}",
		"{% members only if user.email == \"a\" %}": "{% members only if user.email == \"a\" %}",
		"{% membersonly %}":                         "",
		"{% only members %}":                        "",
	}
	for src, want := range cases {
		if got := FindGate([]byte(src)); got != want {
			t.Errorf("FindGate(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestAccessRulesRequires(t *testing.T) {
	r := AccessRules{MembersOnly: []string{"/members/*", "/downloads", "files/*.pdf"}, EditorsOnly: []string{"/members/staff/*"}, OwnersOnly: []string{"/danger"}}
	cases := map[string]string{
		"/":                   "",
		"/members":            "member",
		"/members/":           "member",
		"/members/a/b":        "member",
		"/membership":         "",
		"/downloads":          "member",
		"/downloads/x":        "",
		"/files/a.pdf":        "member",
		"/members/staff/rota": "editor", // the more demanding rule wins
		"/danger":             "owner",
	}
	for p, want := range cases {
		if got := r.Requires(p); got != want {
			t.Errorf("Requires(%q) = %q, want %q", p, got, want)
		}
	}
}
