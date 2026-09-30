package renderer

import (
	"reflect"
	"testing"
)

func TestVisitorChatsInTemplate(t *testing.T) {
	src := `
<friendo-chat chat-id="general" visitors-can-chat></friendo-chat>
<friendo-chat chat-id="post-{{ post.slug }}" visitors-can-chat></friendo-chat>
<friendo-chat chat-id="members"></friendo-chat>
<friendo-chat chat-id="{{ post.slug }}" visitors-can-chat></friendo-chat>
<friendo-chat chat-id="board" group="board" visitors-can-chat></friendo-chat>
<friendo-chat visitors-can-chat></friendo-chat>`
	patterns, skipped := VisitorChatsInTemplate(src)
	if want := []string{"general", "post-{{ post.slug }}"}; !reflect.DeepEqual(patterns, want) {
		t.Errorf("patterns = %v, want %v", patterns, want)
	}
	if len(skipped) != 3 {
		t.Errorf("skipped = %v, want the bare-template, group and id-less tags", skipped)
	}
	re, _ := ChatPatternRegexp("post-{{ post.slug }}")
	for id, want := range map[string]bool{"post-hello": true, "post-": false, "general": false, "post-a/b": false, "xpost-hello": false} {
		if re.MatchString(id) != want {
			t.Errorf("%q matched = %v, want %v", id, !want, want)
		}
	}
}
