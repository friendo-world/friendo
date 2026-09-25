package deploy

import (
	"os"
	"path/filepath"
	"strings"
)

// A site's collections and fields can be started ad hoc — in the admin, from a
// form, from a content/ folder — so the deployed site can know about more than
// the folder's friendo.toml says. On pull, the runtime hands back the [content]
// block that matches the site as it is, and it's written over the local file's
// [content] section; every other section, and every comment outside it, stays.

// PullContentToml fetches the [content] block that matches the site as it is.
func (c *SiteClient) PullContentToml() (string, error) {
	var result struct {
		Toml string `json:"toml"`
	}
	if err := c.get("/_/api/content/toml", &result); err != nil {
		return "", err
	}
	return result.Toml, nil
}

// writeContentBlock replaces friendo.toml's [content] section (the [content]
// header and every [content.*] table after it, up to the next other section)
// with `block`, or appends the block when the file has no [content] yet.
// Returns whether the file changed.
func writeContentBlock(siteDir, block string) (bool, error) {
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	raw, err := os.ReadFile(tomlPath)
	if err != nil {
		return false, err
	}
	updated := replaceContentBlock(string(raw), block)
	if updated == string(raw) {
		return false, nil
	}
	return true, os.WriteFile(tomlPath, []byte(updated), 0o644)
}

// isContentHeader matches "[content]" and "[content.blog.fields]" lines.
func isContentHeader(line string) bool {
	t := strings.TrimSpace(line)
	return t == "[content]" || strings.HasPrefix(t, "[content.")
}

// isSectionHeader matches any "[section]" line.
func isSectionHeader(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")
}

func replaceContentBlock(file, block string) string {
	block = strings.TrimRight(block, "\n") + "\n"
	lines := strings.Split(file, "\n")

	// Find the [content] section: from its header to the line before the next
	// section that isn't one of its own tables. Comments right above the next
	// section belong to that section, so they're kept out of the replaced span.
	start, end := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "[content]" {
			start = i
			break
		}
	}
	if start == -1 {
		out := strings.TrimRight(file, "\n")
		if out != "" {
			out += "\n\n"
		}
		return out + block
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isSectionHeader(lines[i]) && !isContentHeader(lines[i]) {
			end = i
			break
		}
	}
	// Give back the comments and blank lines that sit between the last content
	// line and the next section, so the spacing before it is untouched.
	for end > start+1 {
		t := strings.TrimSpace(lines[end-1])
		if t == "" || strings.HasPrefix(t, "#") {
			end--
			continue
		}
		break
	}

	var out []string
	out = append(out, lines[:start]...)
	out = append(out, strings.Split(strings.TrimRight(block, "\n"), "\n")...)
	rest := lines[end:]
	if len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
		out = append(out, "")
	}
	out = append(out, rest...)
	return strings.Join(out, "\n")
}
