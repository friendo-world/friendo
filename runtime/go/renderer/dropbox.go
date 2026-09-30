package renderer

import (
	"regexp"
	"strings"
)

// Drop boxes named in markup: <friendo-form collection="tips" drop-box> in a
// template makes `tips` a drop box — anyone can post there and no one's name is
// kept (see api/dropbox.go). It's read from the template files themselves, never
// from a rendered page: a rendered page holds what people wrote, and a post body
// must not be able to open a collection to everyone.

var (
	formTag       = regexp.MustCompile(`(?is)<friendo-form\b([^>]*)>`)
	dropBoxAttr   = regexp.MustCompile(`(?i)(?:^|\s)drop-box(?:\s|=|$)`)
	collectionVal = regexp.MustCompile(`(?i)(?:^|\s)collection\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// DropBoxesInTemplate returns the collections a template's forms mark drop-box,
// and the forms it had to skip because their collection isn't written out
// literally (collection="{{ … }}" can't be known ahead of time). A form with no
// collection attribute posts to "posts", as <friendo-form> does.
func DropBoxesInTemplate(src string) (boxes []string, skipped []string) {
	for _, m := range formTag.FindAllStringSubmatch(src, -1) {
		attrs := m[1]
		if !dropBoxAttr.MatchString(attrs) {
			continue
		}
		name := "posts"
		if c := collectionVal.FindStringSubmatch(attrs); c != nil {
			name = strings.TrimSpace(c[1] + c[2] + c[3])
		}
		if name == "" || strings.Contains(name, "{{") || strings.Contains(name, "{%") {
			skipped = append(skipped, strings.TrimSpace(m[0]))
			continue
		}
		boxes = append(boxes, name)
	}
	return boxes, skipped
}
