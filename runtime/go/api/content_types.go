package api

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/friendo-world/friendo/runtime/go/data"
)

// friendo.toml's [content] block names the site's content types and, optionally,
// the fields each one's records carry:
//
//	[content]
//	types = ["blog", "events"]
//
//	[content.blog.fields]
//	tags  = "tags"
//	cover = "image"
//	mood  = { kind = "text", choices = ["calm", "wild"], required = true, hint = "How it feels" }
//
// A bare string is the field's kind; a table adds choices, required and a hint.
// The runtime only reads and serves these — the admin uses them to lay out its
// table and form, and to check a record before saving. The records API itself
// accepts any data, so content/ files, <friendo-form> and the CLI are unaffected.

// FieldDecl is one declared field of a content type.
type FieldDecl struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Choices  []string `json:"choices,omitempty"`
	Required bool     `json:"required,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

// ContentType is one entry of [content] types with its declared fields.
type ContentType struct {
	Name   string
	Fields []FieldDecl
}

// fieldKinds are the words a field's kind may be, as the admin's widgets know them.
var fieldKinds = map[string]bool{
	"text": true, "paragraph": true, "number": true, "checkbox": true, "tags": true, "image": true, "json": true,
}

// columnNames are a record's own fields, which a declared field may not shadow.
var columnNames = map[string]bool{
	"title": true, "slug": true, "body": true, "status": true, "id": true, "collection": true,
	"created": true, "updated": true, "published_at": true, "author_id": true,
}

// fieldTable is the long form of a field declaration.
type fieldTable struct {
	Kind     string   `toml:"kind"`
	Choices  []string `toml:"choices"`
	Required bool     `toml:"required"`
	Hint     string   `toml:"hint"`
}

// loadContentTypes reads [content] from friendo.toml. It returns the declared
// types in the order `types` lists them, each with its fields in file order, and
// whether the site declares any types at all (a site without the key falls back
// to whatever collections have records).
func loadContentTypes(siteDir string) ([]ContentType, bool) {
	if siteDir == "" {
		return nil, false
	}
	path := filepath.Join(siteDir, "friendo.toml")
	if _, err := os.Stat(path); err != nil {
		return nil, false
	}
	var raw struct {
		Content map[string]toml.Primitive `toml:"content"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		log.Printf("friendo.toml: could not read [content]: %v", err)
		return nil, false
	}
	typesPrim, ok := raw.Content["types"]
	if !ok {
		return nil, false
	}
	var names []string
	if err := md.PrimitiveDecode(typesPrim, &names); err != nil {
		log.Printf("friendo.toml: [content] types should be a list of names: %v", err)
		return nil, false
	}
	if len(names) == 0 {
		return nil, false
	}

	// Field order is the order the keys appear in the file.
	order := map[string][]string{}
	for _, k := range md.Keys() {
		if len(k) == 4 && k[0] == "content" && k[2] == "fields" {
			order[k[1]] = append(order[k[1]], k[3])
		}
	}

	out := make([]ContentType, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		ct := ContentType{Name: name}
		if prim, ok := raw.Content[name]; ok {
			var section struct {
				Fields map[string]toml.Primitive `toml:"fields"`
			}
			if err := md.PrimitiveDecode(prim, &section); err != nil {
				log.Printf("friendo.toml: [content.%s] could not be read: %v", name, err)
			}
			for _, fname := range order[name] {
				fprim, ok := section.Fields[fname]
				if !ok {
					continue
				}
				if f, ok := decodeField(md, name, fname, fprim); ok {
					ct.Fields = append(ct.Fields, f)
				}
			}
		}
		out = append(out, ct)
	}
	for name := range raw.Content {
		if name != "types" && !seen[name] {
			log.Printf("friendo.toml: [content.%s] is declared but %q is not in [content] types", name, name)
		}
	}
	return out, true
}

// decodeField reads one field, written as a kind ("tags") or a table.
func decodeField(md toml.MetaData, typeName, fname string, prim toml.Primitive) (FieldDecl, bool) {
	f := FieldDecl{Name: fname}
	var kind string
	if err := md.PrimitiveDecode(prim, &kind); err == nil {
		f.Kind = kind
	} else {
		var t fieldTable
		if err := md.PrimitiveDecode(prim, &t); err != nil {
			log.Printf("friendo.toml: [content.%s.fields] %s should be a kind like \"text\" or a table with kind = …: %v", typeName, fname, err)
			return f, false
		}
		f.Kind, f.Choices, f.Required, f.Hint = t.Kind, t.Choices, t.Required, t.Hint
	}
	f.Kind = strings.ToLower(strings.TrimSpace(f.Kind))
	if f.Kind == "" {
		f.Kind = "text"
	}
	switch {
	case !fieldKinds[f.Kind]:
		log.Printf("friendo.toml: [content.%s.fields] %s: unknown kind %q (use text, paragraph, number, checkbox, tags, image or json)", typeName, fname, f.Kind)
		return f, false
	case isWhenKey(fname):
		log.Printf("friendo.toml: [content.%s.fields] %s: that name is used by an event's time (the When section)", typeName, fname)
		return f, false
	case columnNames[fname]:
		log.Printf("friendo.toml: [content.%s.fields] %s: that name is one of a record's own fields", typeName, fname)
		return f, false
	}
	return f, true
}

func isWhenKey(name string) bool {
	for _, k := range data.WhenKeys {
		if k == name {
			return true
		}
	}
	return false
}

// collectionInfo is one entry of GET /collections: the name, how many records it
// holds, and — for a declared type — its fields.
type collectionInfo struct {
	Name     string      `json:"name"`
	Count    int         `json:"count"`
	Declared bool        `json:"declared"` // named in friendo.toml's [content] types
	Fields   []FieldDecl `json:"fields"`
}

// --- The [content] block, written back from the site as it is ---

// A collection can be started ad hoc — from the admin, a form, or a content/
// folder — so a site's friendo.toml can fall behind what it actually holds.
// SuggestedContentToml writes the [content] block that matches the live site:
// every collection (the declared ones first, in their order), and each one's
// fields — the declared ones as written, then the ones its records carry, with a
// kind guessed from the values. The admin shows it to copy, and `friendo pull`
// writes it into the local file.
func SuggestedContentToml(db *data.DB, types []ContentType) (string, error) {
	counts, err := db.CollectionCounts()
	if err != nil {
		return "", err
	}
	var names []string
	seen := map[string]bool{}
	for _, t := range types {
		names = append(names, t.Name)
		seen[t.Name] = true
	}
	var adhoc []string
	for _, c := range counts {
		if !seen[c.Name] {
			adhoc = append(adhoc, c.Name)
		}
	}
	sort.Strings(adhoc)
	names = append(names, adhoc...)
	declared := map[string]ContentType{}
	for _, t := range types {
		declared[t.Name] = t
	}

	var b strings.Builder
	b.WriteString("[content]\ntypes = [")
	for i, n := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(n))
	}
	b.WriteString("]\n")

	for _, n := range names {
		fields := declared[n].Fields
		have := map[string]bool{}
		for _, f := range fields {
			have[f.Name] = true
		}
		records, err := db.QueryCollection(n)
		if err != nil {
			return "", err
		}
		for _, f := range inferFieldsFromRecords(records) {
			if !have[f.Name] {
				fields = append(fields, f)
			}
		}
		if len(fields) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n[content.%s.fields]\n", n)
		for _, f := range fields {
			fmt.Fprintf(&b, "%s = %s\n", f.Name, f.tomlValue())
		}
	}
	return b.String(), nil
}

// tomlValue writes a field the short way when a kind is all it has.
func (f FieldDecl) tomlValue() string {
	if len(f.Choices) == 0 && !f.Required && f.Hint == "" {
		return strconv.Quote(f.Kind)
	}
	parts := []string{"kind = " + strconv.Quote(f.Kind)}
	if len(f.Choices) > 0 {
		q := make([]string, len(f.Choices))
		for i, c := range f.Choices {
			q[i] = strconv.Quote(c)
		}
		parts = append(parts, "choices = ["+strings.Join(q, ", ")+"]")
	}
	if f.Required {
		parts = append(parts, "required = true")
	}
	if f.Hint != "" {
		parts = append(parts, "hint = "+strconv.Quote(f.Hint))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

var imageURLRe = regexp.MustCompile(`(?i)^(/assets/|https?://)\S+\.(png|jpe?g|gif|webp|avif|svg)(\?\S*)?$`)

// kindOfValue guesses a field's kind from one value; "" means no opinion.
func kindOfValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return "checkbox"
	case float64, int, int64:
		return "number"
	case string:
		if imageURLRe.MatchString(x) {
			return "image"
		}
		if strings.Contains(x, "\n") || len(x) > 120 {
			return "paragraph"
		}
		return "text"
	case []any:
		for _, e := range x {
			if _, ok := e.(string); !ok {
				return "json"
			}
		}
		return "tags"
	default:
		return "json"
	}
}

// inferFieldsFromRecords reads the fields a collection's records carry, ordered
// by how many records have each, then by name — the same rule the admin uses.
func inferFieldsFromRecords(records []map[string]any) []FieldDecl {
	type stat struct {
		count int
		votes map[string]bool
	}
	stats := map[string]*stat{}
	for _, r := range records {
		d, ok := r["data"].(map[string]any)
		if !ok {
			continue
		}
		for k, v := range d {
			if isWhenKey(k) || columnNames[k] {
				continue
			}
			s := stats[k]
			if s == nil {
				s = &stat{votes: map[string]bool{}}
				stats[k] = s
			}
			s.count++
			if kind := kindOfValue(v); kind != "" {
				s.votes[kind] = true
			}
		}
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := stats[keys[i]], stats[keys[j]]
		if a.count != b.count {
			return a.count > b.count
		}
		return keys[i] < keys[j]
	})
	out := make([]FieldDecl, 0, len(keys))
	for _, k := range keys {
		out = append(out, FieldDecl{Name: k, Kind: settleKind(stats[k].votes)})
	}
	return out
}

// settleKind picks one kind from those a field's values were seen as.
func settleKind(votes map[string]bool) string {
	if len(votes) == 0 {
		return "text"
	}
	if len(votes) == 1 {
		for k := range votes {
			return k
		}
	}
	only := func(allowed ...string) bool {
		for k := range votes {
			ok := false
			for _, a := range allowed {
				if a == k {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}
	switch {
	case only("text", "paragraph"):
		return "paragraph"
	case only("text", "image"):
		return "text"
	case votes["json"] || votes["tags"]:
		return "json"
	}
	return "text"
}

// handleContentToml serves the suggested [content] block for the admin to copy
// and for `friendo pull` to write into the local friendo.toml.
func handleContentToml(db *data.DB, types []ContentType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := SuggestedContentToml(db, types)
		if err != nil {
			jsonError(w, fmt.Sprintf("query error: %v", err), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]any{"toml": out})
	}
}
