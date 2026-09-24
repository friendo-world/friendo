package renderer

import (
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/flosch/pongo2/v6"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// Members-only pages.
//
// A page declares who may see it with one tag:
//
//	{% members only %}                       anyone signed in
//	{% editors only %}                       editor and up (contributors/admins/owners too)
//	{% members only if user.email == "…" %}  signed in AND the expression is true
//
// The tag can sit at the top of a page, inside a block, or in a layout. One node
// type (gateNode) does every check; the server runs it in two ways:
//
//   - Before rendering, on the tag text FindGate pulled out of the page file. This
//     is needed because pongo2 only executes the root layout's document, so a tag
//     at the top of a page that {% extends %} a layout would parse but never run.
//   - During the real render, for a tag that lives in a layout or a block. Either
//     way a failed check surfaces as a *GateError inside the pongo2 error.
//
// The "if" tail is an ordinary template expression over the context (user, record,
// collections…). That's the extension point for finer rules: a groups feature that
// adds user.groups to the context needs no new grammar — `{% members only if
// "book-club" in user.groups %}` works the day the data exists.

// GateError says a viewer may not see a page. Reason is "signin" (nobody is signed
// in), "role" (signed in, but below the required role) or "condition" (the tag's
// `if` expression was false).
type GateError struct {
	Role   string // the minimum role the page asked for, e.g. "editor"
	Reason string // signin | role | condition
}

func (e *GateError) Error() string {
	return fmt.Sprintf("%ss only (%s)", e.Role, e.Reason)
}

// gateRoles maps the tag's plural spelling to the role it requires.
var gateRoles = map[string]string{
	"members":      "member",
	"contributors": "contributor",
	"editors":      "editor",
	"admins":       "admin",
	"owners":       "owner",
}

// gateTagRe finds a gate tag in raw template text. It is deliberately simple: it
// also matches a tag inside a {# comment #}, so the bias is toward over-gating —
// the safe direction. Keep the spelling in one place: this regexp and gateRoles.
var gateTagRe = regexp.MustCompile(`{%-?\s*(members|contributors|editors|admins|owners)\s+only\b[^%]*%}`)

// FindGate returns the first gate tag in a page's source, verbatim, or "" if the
// page is public. The route table and static export call it so a page's gate is
// known without rendering it.
func FindGate(src []byte) string {
	return string(gateTagRe.Find(src))
}

// CompileGate turns a tag's text (from FindGate) into a one-line template whose
// only job is to run the gate check against a context. A bad `if` expression is
// reported here, at route-build time, like any other template error.
func CompileGate(tagText string) (*pongo2.Template, error) {
	return pongo2.FromString(tagText)
}

// CheckGate runs a compiled gate against the page's context. nil means the viewer
// may see the page.
func CheckGate(gate *pongo2.Template, ctx pongo2.Context) (*GateError, error) {
	err := gate.ExecuteWriter(ctx, io.Discard)
	if err == nil {
		return nil, nil
	}
	if ge := GateErrorFrom(err); ge != nil {
		return ge, nil
	}
	return nil, err
}

// GateErrorFrom unwraps the *GateError inside a pongo2 render error, or nil if
// the error was something else. (*pongo2.Error has no Unwrap, hence the two steps.)
func GateErrorFrom(err error) *GateError {
	var pe *pongo2.Error
	if errors.As(err, &pe) {
		err = pe.OrigError
	}
	var ge *GateError
	if errors.As(err, &ge) {
		return ge
	}
	return nil
}

// CheckRole is the role half of the check, shared by the tag and by friendo.toml's
// [access] rules (which have no template to run). user is the template-context
// shape (nil when signed out).
func CheckRole(user map[string]any, required string) *GateError {
	if user == nil {
		return &GateError{Role: required, Reason: "signin"}
	}
	role, _ := user["role"].(string)
	if !data.RoleAtLeast(role, required) {
		return &GateError{Role: required, Reason: "role"}
	}
	return nil
}

// gateNode is what `{% <role>s only [if expr] %}` parses to.
type gateNode struct {
	role  string
	cond  pongo2.IEvaluator // nil when there's no "if"
	start *pongo2.Token
}

func (n *gateNode) Execute(ctx *pongo2.ExecutionContext, w pongo2.TemplateWriter) *pongo2.Error {
	user, _ := ctx.Public["user"].(map[string]any)
	if ge := CheckRole(user, n.role); ge != nil {
		return n.fail(ge)
	}
	if n.cond != nil {
		v, err := n.cond.Evaluate(ctx)
		if err != nil {
			return err
		}
		if !v.IsTrue() {
			return n.fail(&GateError{Role: n.role, Reason: "condition"})
		}
	}
	return nil
}

func (n *gateNode) fail(ge *GateError) *pongo2.Error {
	return &pongo2.Error{Sender: "tag:" + n.role + "s only", OrigError: ge, Token: n.start}
}

// gateTagParser builds the parser for one spelling ("members", "editors", …).
func gateTagParser(role string) pongo2.TagParser {
	return func(doc *pongo2.Parser, start *pongo2.Token, args *pongo2.Parser) (pongo2.INodeTag, *pongo2.Error) {
		node := &gateNode{role: role, start: start}
		if args.Match(pongo2.TokenIdentifier, "only") == nil {
			return nil, args.Error(fmt.Sprintf("expected {%% %ss only %%}", role), start)
		}
		if args.Match(pongo2.TokenIdentifier, "if") != nil {
			cond, err := args.ParseExpression()
			if err != nil {
				return nil, err
			}
			node.cond = cond
		}
		if args.Remaining() > 0 {
			return nil, args.Error(fmt.Sprintf("{%% %ss only %%} takes nothing else (or `if <expression>`)", role), args.Current())
		}
		return node, nil
	}
}

// RegisterGateTags registers the five spellings. Called from init.
func RegisterGateTags() {
	for plural, role := range gateRoles {
		pongo2.RegisterTag(plural, gateTagParser(role))
	}
}

// AccessRules is friendo.toml's [access] table: URL patterns per minimum role, so
// a whole folder can be members-only without a tag on each page.
//
//	[access]
//	members_only = ["/members/*", "/downloads"]
//	editors_only = ["/newsroom/*"]
//
// A pattern ending in "/*" covers the path itself and everything under it; any
// other pattern is matched whole (shell-style "*" and "?" allowed within a segment).
type AccessRules struct {
	MembersOnly      []string `toml:"members_only"`
	ContributorsOnly []string `toml:"contributors_only"`
	EditorsOnly      []string `toml:"editors_only"`
	AdminsOnly       []string `toml:"admins_only"`
	OwnersOnly       []string `toml:"owners_only"`
}

// Requires returns the highest role any matching rule names, or "" for a public path.
func (r AccessRules) Requires(urlPath string) string {
	if len(urlPath) > 1 {
		urlPath = strings.TrimSuffix(urlPath, "/")
	}
	best := ""
	for _, rule := range []struct {
		role  string
		globs []string
	}{
		{"member", r.MembersOnly}, {"contributor", r.ContributorsOnly}, {"editor", r.EditorsOnly},
		{"admin", r.AdminsOnly}, {"owner", r.OwnersOnly},
	} {
		for _, g := range rule.globs {
			if matchPath(g, urlPath) && data.RoleRank(rule.role) > data.RoleRank(best) {
				best = rule.role
			}
		}
	}
	return best
}

func matchPath(pattern, urlPath string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return urlPath == prefix || strings.HasPrefix(urlPath, prefix+"/")
	}
	ok, _ := path.Match(pattern, urlPath)
	return ok
}
