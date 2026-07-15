// SQL-splitter parity: the schema/migration statement splitter runs in both
// runtimes (Go data.SplitStatements, edge sql-split.js splitStatements) and they
// must split identically — a divergence would mean one runtime silently drops or
// mangles a statement at provisioning time. The same splitter-cases.json runs here
// and in edge-parity.mjs. Guards the historical comment-led-file bug from returning.
package tests

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/friendo-world/friendo/runtime/go/data"
)

type splitterCase struct {
	Name   string   `json:"name"`
	SQL    string   `json:"sql"`
	Expect []string `json:"expect"`
}

func TestSplitterParity(t *testing.T) {
	raw, err := os.ReadFile("splitter-cases.json")
	if err != nil {
		t.Fatalf("reading splitter-cases.json: %v", err)
	}
	var cases []splitterCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parsing splitter-cases.json: %v", err)
	}

	for _, c := range cases {
		got := data.SplitStatements(c.SQL)
		if len(got) != len(c.Expect) {
			t.Fatalf("%s: got %d statements, want %d\n got: %#v\nwant: %#v", c.Name, len(got), len(c.Expect), got, c.Expect)
		}
		for i := range got {
			if got[i] != c.Expect[i] {
				t.Fatalf("%s: statement %d\n got: %q\nwant: %q", c.Name, i, got[i], c.Expect[i])
			}
		}
	}
}
