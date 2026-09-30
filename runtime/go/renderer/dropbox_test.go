package renderer

import (
	"reflect"
	"testing"
)

func TestDropBoxesInTemplate(t *testing.T) {
	src := `
<friendo-form collection="tips" drop-box>…</friendo-form>
<friendo-form collection='feedback' class="x" drop-box="">…</friendo-form>
<friendo-form drop-box>…</friendo-form>
<friendo-form collection="blog">…</friendo-form>
<friendo-form collection="stories" data-drop-box-note="x">…</friendo-form>
<friendo-form
    collection="multi"
    drop-box
>…</friendo-form>
<friendo-form collection="{{ c }}" drop-box>…</friendo-form>`
	boxes, skipped := DropBoxesInTemplate(src)
	if want := []string{"tips", "feedback", "posts", "multi"}; !reflect.DeepEqual(boxes, want) {
		t.Errorf("boxes = %v, want %v", boxes, want)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want the templated one", skipped)
	}
}
