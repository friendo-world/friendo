package renderer

import (
	"regexp"
	"strings"
)

// Chats visitors may write in: <friendo-chat chat-id="general" visitors-can-chat>
// in a template lets someone who hasn't signed in post there. Like drop-box, it's
// read from the template files, never a rendered page, so what people write
// can't open a chat.
//
// A chat id may be templated — chat-id="post-{{ post.slug }}" — and then each
// {{ … }} or {% … %} part stands for any id text there, so every post's chat is
// open. At least some of the id must be written out: chat-id="{{ post.slug }}"
// alone would open every chat on the site, so it's skipped (with a reason).

var (
	chatTag          = regexp.MustCompile(`(?is)<friendo-chat\b([^>]*)>`)
	visitorsChatAttr = regexp.MustCompile(`(?i)(?:^|\s)visitors-can-chat(?:\s|=|$)`)
	chatIDVal        = regexp.MustCompile(`(?i)(?:^|\s)chat-id\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	chatGroupVal     = regexp.MustCompile(`(?i)(?:^|\s)group\s*=`)
	templatePart     = regexp.MustCompile(`\{\{.*?\}\}|\{%.*?%\}`)
	literalChatText  = regexp.MustCompile(`^[A-Za-z0-9._-]*$`)
)

// VisitorChatsInTemplate returns the chat-id patterns a template's chats mark
// visitors-can-chat, and a reason for each tag it had to skip.
func VisitorChatsInTemplate(src string) (patterns []string, skipped []string) {
	for _, m := range chatTag.FindAllStringSubmatch(src, -1) {
		attrs := m[1]
		if !visitorsChatAttr.MatchString(attrs) {
			continue
		}
		tag := strings.TrimSpace(m[0])
		if chatGroupVal.MatchString(attrs) {
			skipped = append(skipped, tag+" — a group's chat is for its members")
			continue
		}
		id := chatIDVal.FindStringSubmatch(attrs)
		if id == nil {
			skipped = append(skipped, tag+" — it has no chat-id")
			continue
		}
		pattern := strings.TrimSpace(id[1] + id[2])
		if _, err := ChatPatternRegexp(pattern); err != nil {
			skipped = append(skipped, tag+" — "+err.Error())
			continue
		}
		patterns = append(patterns, pattern)
	}
	return patterns, skipped
}

type chatPatternError string

func (e chatPatternError) Error() string { return string(e) }

// ChatPatternRegexp turns a chat-id pattern into the matcher for chat ids.
func ChatPatternRegexp(pattern string) (*regexp.Regexp, error) {
	literals := templatePart.Split(pattern, -1)
	fixed := ""
	var b strings.Builder
	b.WriteString("^")
	for i, lit := range literals {
		if !literalChatText.MatchString(lit) {
			return nil, chatPatternError("chat ids use only letters, digits, . _ and -")
		}
		fixed += lit
		b.WriteString(regexp.QuoteMeta(lit))
		if i < len(literals)-1 {
			b.WriteString(`[A-Za-z0-9._-]+`)
		}
	}
	b.WriteString("$")
	if fixed == "" {
		return nil, chatPatternError(`write part of the chat-id out, like chat-id="post-{{ post.slug }}", or every chat on the site would open`)
	}
	return regexp.Compile(b.String())
}
