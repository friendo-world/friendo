---
title: Templates
slug: templates
group: Concepts
weight: 6
---

Friendo templates are [Pongo2](https://github.com/flosch/pongo2) — a
Jinja2-compatible language — as plain `.html` files with logic. The edge runtime
uses a matching Jinja2-compatible engine, so templates render the same way
locally and when deployed.

## Layouts and blocks

Put shared chrome in `templates/` and extend it from pages:

```html
{# templates/base.html #}
<!DOCTYPE html>
<html>
  <head><title>{% block title %}{{ site.name }}{% endblock %}</title></head>
  <body>{% block content %}{% endblock %}</body>
</html>
```

```html
{# pages/index.html #}
{% extends "templates/base.html" %}
{% block content %}<h1>Hello</h1>{% endblock %}
```

## Pages and URLs

Every file in `pages/` maps to a URL by its path: `pages/about.html` → `/about`,
`pages/blog/index.html` → `/blog`.

### Dynamic routes

A path segment in square brackets is a parameter, and the **directory name is the
collection** it looks up. So `pages/blog/[slug].html` serves `/blog/:slug` and
matches a record in the `blog` collection by its `slug` — available as `record`:

```html
{# pages/blog/[slug].html #}
{% extends "templates/base.html" %}
{% block content %}
  <h1>{{ record.title }}</h1>
  <div>{{ record.body|markdown }}</div>
{% endblock %}
```

## Template context

Every page is rendered with:

| Variable | What |
|---|---|
| `site.name` | Your site name from `friendo.toml` |
| `request.path` | The current URL path |
| `collections.<name>` | All records in a collection |
| `record` | The matched record on a dynamic `[param]` route |

## Filters

Filters transform values with `|`. Friendo adds these to the standard Pongo2/Jinja
set (`truncate`, `upper`, `lower`, `date`, `default`, `length`, …):

| Filter | Example | Result |
|---|---|---|
| `asset_url` | `{{ "logo.png"\|asset_url }}` | `/public/logo.png` |
| `date` | `{{ post.created\|date:"Jan 2, 2006" }}` | a formatted date |
| `resize` | `{{ img\|asset_url\|resize:"300x200" }}` | `/public/img?w=300&h=200` (a CDN hint) |
| `markdown` | `{{ post.body\|markdown }}` | markdown rendered to HTML |

### markdown

The `markdown` filter renders a markdown string to HTML (GitHub-flavored: tables,
strikethrough, autolinks, task lists) and marks the result safe, so it isn't
escaped:

```html
<article>{{ record.body|markdown }}</article>
```

This is how these docs are rendered — each page is a markdown record in a `docs`
collection, output through `{{ record.body|markdown }}`.

### resize

`resize` appends `w`/`h` query hints (`"W"`, `"WxH"`, or `"xH"`). An image CDN
like Cloudflare Image Resizing honors them; the built-in static server ignores
them and serves the original, so it degrades gracefully.
