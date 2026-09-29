package renderer

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	gmrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// fenceInfoRenderer renders fenced code blocks exactly as goldmark does, and
// additionally keeps the whole info string when it says more than the language.
//
//	```bash tab="macOS / Linux" group=install
//
// becomes <pre data-info="bash tab=&quot;macOS / Linux&quot; group=install"><code class="language-bash">.
// A fence with only a language (or none) renders byte-for-byte as before, so
// no existing site changes. A site's stylesheet or script can read data-info
// to build tabs, titles or whatever else the author meant by it.
type fenceInfoRenderer struct{}

func (fenceInfoRenderer) RegisterFuncs(reg gmrenderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, renderFencedCodeBlock)
}

func renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.FencedCodeBlock)
	if !entering {
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<pre")
	language := n.Language(source)
	if n.Info != nil {
		info := bytes.TrimSpace(n.Info.Segment.Value(source))
		if len(info) > len(language) {
			_, _ = w.WriteString(` data-info="`)
			html.DefaultWriter.RawWrite(w, info)
			_, _ = w.WriteString(`"`)
		}
	}
	_, _ = w.WriteString("><code")
	if language != nil {
		_, _ = w.WriteString(` class="language-`)
		html.DefaultWriter.Write(w, language)
		_, _ = w.WriteString(`"`)
	}
	_ = w.WriteByte('>')
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		html.DefaultWriter.RawWrite(w, line.Value(source))
	}
	return ast.WalkContinue, nil
}
