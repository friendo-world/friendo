package renderer

import (
	"bytes"
	"strings"
	"testing"
)

func render(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestHeadingIDs(t *testing.T) {
	out := render(t, "## Hello World\n\ntext\n\n### Hello World\n")
	for _, want := range []string{`<h2 id="hello-world">`, `<h3 id="hello-world-1">`} {
		if !strings.Contains(out, want) {
			t.Errorf("want %s in:\n%s", want, out)
		}
	}
}

func TestFenceInfo(t *testing.T) {
	cases := []struct{ src, want, not string }{
		{"```bash\necho hi\n```\n", `<pre><code class="language-bash">echo hi` + "\n</code></pre>", "data-info"},
		{"```\nplain\n```\n", "<pre><code>plain\n</code></pre>", "data-info"},
		{"```bash tab=\"macOS\" group=install\n<x>\n```\n", `<pre data-info="bash tab=&quot;macOS&quot; group=install"><code class="language-bash">&lt;x&gt;` + "\n</code></pre>", ""},
	}
	for _, c := range cases {
		out := render(t, c.src)
		if !strings.Contains(out, c.want) {
			t.Errorf("want %q in %q", c.want, out)
		}
		if c.not != "" && strings.Contains(out, c.not) {
			t.Errorf("did not want %q in %q", c.not, out)
		}
	}
}
