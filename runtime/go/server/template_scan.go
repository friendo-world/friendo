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

// scanTemplates reads every template under pages/ and layouts/ for
// <friendo-chat … visitors-can-chat> (the chats visitors may write in) and
// <friendo-form … drop-box>, and records the collections they name (see
// renderer.DropBoxesInTemplate), so the API treats them as drop boxes. Run when
// the site is built and on every reload, so removing the attribute closes the
// collection again — which is said loudly, since posts to it would start keeping
// their sender's name.
func scanTemplates(siteDir string, db *data.DB) {
	found := map[string]bool{}
	chats := map[string]bool{}
	for _, dir := range []string{"pages", "layouts"} {
		filepath.Walk(filepath.Join(siteDir, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".html") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(siteDir, path)
			// <friendo-chat … visitors-can-chat>: chats visitors may write in.
			if strings.Contains(string(src), "visitors-can-chat") {
				patterns, skipped := renderer.VisitorChatsInTemplate(string(src))
				for _, p := range patterns {
					chats[p] = true
				}
				for _, why := range skipped {
					log.Printf("%s: ignoring visitors-can-chat on %s", rel, why)
				}
			}
			if !strings.Contains(string(src), "drop-box") {
				return nil
			}
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
	chatPatterns := make([]string, 0, len(chats))
	for p := range chats {
		chatPatterns = append(chatPatterns, p)
	}
	openedChats, closedChats := db.SetVisitorChatPatterns(chatPatterns)
	if len(openedChats) > 0 {
		log.Printf("Chats visitors can write in: %s", strings.Join(openedChats, ", "))
	}
	for _, p := range closedChats {
		log.Printf("Chat %q is members-only again — no <friendo-chat> says visitors-can-chat for it any more.", p)
	}

	added, removed := db.SetPageDropBoxes(names)
	if len(added) > 0 {
		log.Printf("Drop boxes named on pages: %s", strings.Join(added, ", "))
	}
	for _, name := range removed {
		log.Printf("Warning: %q is no longer a drop box — no form says drop-box any more. Posts sent to it from now on keep their sender's name.", name)
	}
}
