package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

// Init creates a new Friendo site directory with the canonical structure.
func Init(name string) error {
	if name == "" {
		name = "my-site"
	}

	// Check if directory already exists.
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	dirs := []string{
		filepath.Join(name, "pages"),
		filepath.Join(name, "pages", "blog"),
		filepath.Join(name, "layouts"),
		filepath.Join(name, "assets"),
		filepath.Join(name, "content", "blog"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(name, "friendo.toml"):                 friendoToml(name),
		filepath.Join(name, "layouts", "base.html"):         baseHTML,
		filepath.Join(name, "pages", "index.html"):          indexHTML,
		filepath.Join(name, "pages", "404.html"):            notFoundHTML,
		filepath.Join(name, "pages", "blog", "[slug].html"): blogPostHTML,
		filepath.Join(name, "assets", "style.css"):          styleCSS,
		filepath.Join(name, "content", "blog", "hello-world.md"): helloPostMD,
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return nil
}

func friendoToml(name string) string {
	return fmt.Sprintf(`[site]
name = "%s"

[content]
types = ["blog", "pages"]

[deploy]
# domain = "mysite.com"
`, name)
}

const baseHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ site.name }}</title>
    <link rel="stylesheet" href="/assets/style.css">
</head>
<body>
    <nav>
        <a href="/">{{ site.name }}</a>
    </nav>
    <main>
        {% block content %}{% endblock %}
    </main>
</body>
</html>
`

const indexHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>Welcome to {{ site.name }}</h1>

{% for post in collections.blog %}
<article>
    <h2><a href="/blog/{{ post.slug }}">{{ post.title }}</a></h2>
    <p>{{ post.body }}</p>
</article>
{% empty %}
<p>No blog posts yet. Create one in the <a href="/_/">admin UI</a>.</p>
{% endfor %}
{% endblock %}
`

const notFoundHTML = `{% extends "layouts/base.html" %}
{% block content %}
<h1>404 — Page not found</h1>
<p>There's nothing at <code>{{ request.path }}</code>.</p>
<p><a href="/">Go home</a></p>
{% endblock %}
`

const blogPostHTML = `{% extends "layouts/base.html" %}
{% block content %}
<article>
    <h1>{{ record.title }}</h1>
    <div>{{ record.body|markdown }}</div>
    <p><a href="/">&larr; Back</a></p>
</article>
{% endblock %}
`

// helloPostMD is a starter content file. Running ` + "`friendo build`" + ` (or
// ` + "`friendo serve`" + `) compiles content/blog/*.md into the blog collection.
const helloPostMD = `---
title: Hello, world
slug: hello-world
---

Welcome to your new **Friendo** site.

This post lives in ` + "`content/blog/hello-world.md`" + ` — a plain markdown file.
Edit it, add more files under ` + "`content/`" + `, and run ` + "`friendo serve`" + ` to see
changes live. The folder under ` + "`content/`" + ` is the collection; the front matter
above sets the title and slug.
`

const styleCSS = `/* Friendo starter styles */
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 48rem; margin: 0 auto; padding: 2rem; }
nav { margin-bottom: 2rem; padding-bottom: 1rem; border-bottom: 1px solid #e5e7eb; }
nav a { font-weight: 700; color: #333; text-decoration: none; }
h1 { margin-bottom: 1rem; }
h2 { margin-bottom: 0.5rem; }
article { margin-bottom: 2rem; }
a { color: #2563eb; }
code { background: #f3f4f6; padding: 0.15rem 0.3rem; border-radius: 3px; font-size: 0.9em; }
`
