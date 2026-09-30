package server

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/friendo-world/friendo/runtime/go/data"
	"github.com/friendo-world/friendo/runtime/go/renderer"
)

// scanDropBoxes reads every template under pages/ and layouts/ for
// <friendo-form … drop-box> and records the collections they name (see
// renderer.DropBoxesInTemplate), so the API treats them as drop boxes. Run when
// the site is built and on every reload, so removing the attribute closes the
// collection again — which is said loudly, since posts to it would start keeping
// their sender's name.
func scanDropBoxes(siteDir string, db *data.DB) {
	found := map[string]bool{}
	for _, dir := range []string{"pages", "layouts"} {
		filepath.Walk(filepath.Join(siteDir, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".html") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(src), "drop-box") {
				return nil
			}
			rel, _ := filepath.Rel(siteDir, path)
			boxes, skipped := renderer.DropBoxesInTemplate(string(src))
			for _, name := range boxes {
				if name == data.GroupsCollection || name == "profiles" {
					log.Printf("%s: %q can't be a drop box — ignoring drop-box", rel, name)
					continue
				}
				found[name] = true
			}
			for _, tag := range skipped {
				log.Printf("%s: drop-box needs the collection written out, like collection=\"tips\" — ignoring %s", rel, tag)
			}
			return nil
		})
	}
	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	added, removed := db.SetPageDropBoxes(names)
	if len(added) > 0 {
		log.Printf("Drop boxes named on pages: %s", strings.Join(added, ", "))
	}
	for _, name := range removed {
		log.Printf("Warning: %q is no longer a drop box — no form says drop-box any more. Posts sent to it from now on keep their sender's name.", name)
	}
}
