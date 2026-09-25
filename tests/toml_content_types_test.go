// Covers friendo.toml's [content] block: declared types come back first, in
// order, with their declared fields (short and long forms); names that would
// clash with a record's own fields or an event's time are dropped; collections
// that exist only by having records follow; a site that declares nothing gets
// the defaults; and the suggested [content] block reflects all of it.
package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendo-world/friendo/runtime/go/admin"
	"github.com/friendo-world/friendo/runtime/go/data"
)

type collectionsResponse struct {
	Declared    bool `json:"declared"`
	Collections []struct {
		Name     string `json:"name"`
		Count    int    `json:"count"`
		Declared bool   `json:"declared"`
		Fields   []struct {
			Name     string   `json:"name"`
			Kind     string   `json:"kind"`
			Choices  []string `json:"choices"`
			Required bool     `json:"required"`
			Hint     string   `json:"hint"`
		} `json:"fields"`
	} `json:"collections"`
}

// listCollections seeds a recipe and an ad-hoc "notes" record, then returns
// GET /collections and GET /content/toml.
func listCollections(t *testing.T, siteDir string) (collectionsResponse, string) {
	t.Helper()
	db, err := data.Open(siteDir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	id, err := db.CreateRecord("recipes", "soup", "Soup", "", "published", "")
	if err != nil {
		t.Fatalf("seeding a record: %v", err)
	}
	if err := db.SetRecordData(id, `{"serves": 4, "course": "main", "steps": ["chop", "simmer"], "notes": "long\ntext"}`); err != nil {
		t.Fatalf("seeding data: %v", err)
	}
	if _, err := db.CreateRecord("notes", "first", "First", "", "draft", ""); err != nil {
		t.Fatalf("seeding a record: %v", err)
	}
	r := chi.NewRouter()
	admin.Mount(r, db, true, "testsite", siteDir, nil, nil, nil)
	srv := httptest.NewServer(r)
	defer srv.Close()
	get := func(path string) []byte {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("X-Friendo-Admin", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("GET %s = %d", path, res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		return body
	}
	var out collectionsResponse
	if err := json.Unmarshal(get("/_/api/collections"), &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	var tomlOut struct {
		Toml string `json:"toml"`
	}
	if err := json.Unmarshal(get("/_/api/content/toml"), &tomlOut); err != nil {
		t.Fatalf("decoding toml: %v", err)
	}
	return out, tomlOut.Toml
}

func TestTomlContentTypes(t *testing.T) {
	siteDir := t.TempDir()
	toml := `[site]
name = "testsite"

[content]
types = ["recipes", "blog"]

[content.recipes.fields]
serves = "number"
tags = "tags"
photo = { kind = "image", hint = "A picture of the dish" }
course = { kind = "text", choices = ["starter", "main", "pudding"], required = true }
when = "text"
title = "text"
weird = "colour"
`
	if err := os.WriteFile(filepath.Join(siteDir, "friendo.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("writing friendo.toml: %v", err)
	}
	out, suggested := listCollections(t, siteDir)
	if !out.Declared {
		t.Fatal("declared should be true when [content] types is set")
	}
	if len(out.Collections) != 3 || out.Collections[0].Name != "recipes" || out.Collections[1].Name != "blog" || out.Collections[2].Name != "notes" {
		t.Fatalf("collections = %+v, want recipes, blog (declared) then notes (ad hoc)", out.Collections)
	}
	if !out.Collections[0].Declared || out.Collections[2].Declared {
		t.Fatal("declared flags: recipes should be declared, notes not")
	}
	if out.Collections[0].Count != 1 {
		t.Fatalf("recipes count = %d, want 1", out.Collections[0].Count)
	}
	// The suggested block: declared types first, ad-hoc after; declared fields as
	// written, then the ones the records carry with a guessed kind.
	want := `[content]
types = ["recipes", "blog", "notes"]

[content.recipes.fields]
serves = "number"
tags = "tags"
photo = { kind = "image", hint = "A picture of the dish" }
course = { kind = "text", choices = ["starter", "main", "pudding"], required = true }
notes = "paragraph"
steps = "tags"
`
	if suggested != want {
		t.Fatalf("suggested toml =\n%s\nwant\n%s", suggested, want)
	}
	fields := out.Collections[0].Fields
	names := []string{}
	for _, f := range fields {
		names = append(names, f.Name+":"+f.Kind)
	}
	wantFields := "serves:number tags:tags photo:image course:text"
	if got := join(names); got != wantFields {
		t.Fatalf("recipes fields = %q, want %q (file order; when/title/weird dropped)", got, wantFields)
	}
	if fields[2].Hint != "A picture of the dish" {
		t.Fatalf("photo hint = %q", fields[2].Hint)
	}
	if !fields[3].Required || len(fields[3].Choices) != 3 || fields[3].Choices[2] != "pudding" {
		t.Fatalf("course = %+v, want required with three choices", fields[3])
	}
	if len(out.Collections[1].Fields) != 0 {
		t.Fatalf("blog fields = %+v, want none", out.Collections[1].Fields)
	}
}

func TestTomlContentTypesAbsent(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "friendo.toml"), []byte("[site]\nname = \"plain\"\n"), 0o644); err != nil {
		t.Fatalf("writing friendo.toml: %v", err)
	}
	out, suggested := listCollections(t, siteDir)
	if out.Declared {
		t.Fatal("declared should be false without [content] types")
	}
	names := []string{}
	for _, c := range out.Collections {
		names = append(names, c.Name)
	}
	if got := join(names); got != "blog pages posts notes recipes" {
		t.Fatalf("collections = %q, want the defaults then what has records", got)
	}
	if !strings.HasPrefix(suggested, "[content]\ntypes = [\"notes\", \"recipes\"]\n") {
		t.Fatalf("suggested toml without declarations =\n%s", suggested)
	}
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}
