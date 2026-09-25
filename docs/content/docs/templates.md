---
title: Templates
slug: templates
group: Concepts
weight: 6
---

Friendo templates are [Pongo2](https://github.com/flosch/pongo2) — a
Jinja2-compatible language — as plain `.html` files with logic. One runtime
renders them everywhere, so a template looks the same on your laptop, on a
server you run, and on friendo.world.

## Layouts and blocks

Put shared chrome in `layouts/` and extend it from pages:

```html
{# layouts/base.html #}
<!DOCTYPE html>
<html>
  <head><title>{% block title %}{{ site.name }}{% endblock %}</title></head>
  <body>{% block content %}{% endblock %}</body>
</html>
```

```html
{# pages/index.html #}
{% extends "layouts/base.html" %}
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
{% extends "layouts/base.html" %}
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
| `record.data.<field>` | Custom [front matter](/docs/content) fields (tags, weight, …) |
| `record.comments` / `.reactions` / `.poll` / `.gallery` | The post's public [community relations](#community-relations) (server-rendered) |
| `record.when` | The post's time, if it's an [event](/docs/calendar): `starts`, `ends`, `all_day`, `repeats`, `next` — on every record in `collections.*` too |
| `record.rsvps` | The event's [RSVP](/docs/calendar#rsvp-friendo-rsvp) tally for its next date: `going`, `maybe`, `not_going` |
| `record.location` | The post's [map pin](/docs/maps), if any: `lat`, `lng`, `label` |
| `site.url` | The site's own origin as the request saw it (`https://my-site.friendo.world`); in a static export, `[deploy]` from friendo.toml |
| `calendar.google` / `.webcal` / `.ics` | [Subscribe links](/docs/calendar#google-calendar-apple-calendar-outlook) for the site's events |
| `user` | Who's signed in — `name`, `email`, `role`, `id` — or empty for a visitor. See [Members-only pages](#members-only-pages) |
| `gate` | Only on your `login.html`, when it's standing in for a members-only page: `required`, `reason`, `path` |

A record's built-in fields are `id`, `slug`, `title`, `body`, `status`,
`published_at`, `created`, `updated`. Any extra keys from a content file's front
matter are available under `record.data` — e.g. `{{ record.data.weight }}`.

### Community relations

On a dynamic post route, `record` also carries the post's **public community data**,
so you can render it server-side with no JavaScript (the interactive
[`friendo.js` components](/docs/community) are the signed-in path):

| Relation | What |
|---|---|
| `record.comments` | The post's **approved** comments (oldest first): `author_name`, `body`, `created`, … |
| `record.reactions` | Reaction tallies: `{ emoji, count }` per emoji |
| `record.poll` | The post's poll (present only if declared in front matter): `question` + `options` with `text` / `votes` |
| `record.gallery` | Images from the post's [page bundle](/docs/content#page-bundles-galleries): each has a `url` |

```html
<ul>{% for r in record.reactions %}<li>{{ r.emoji }} {{ r.count }}</li>{% endfor %}</ul>
<ol>{% for c in record.comments %}<li><b>{{ c.author_name }}</b>: {{ c.body }}</li>{% endfor %}</ol>
{% for img in record.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

These are attached before the template runs, so they're plain values to loop over.

## Members-only pages

Every page knows who's looking at it. `user` is the signed-in account (anyone who
signed in through [`<friendo-auth>`](/docs/community) or the admin), or empty for a
visitor:

```html
{% if user %}Hi {{ user.name }}{% else %}<friendo-auth reload></friendo-auth>{% endif %}
```

To make a whole page for signed-in people only, put one tag at the top:

```html
{# pages/members.html #}
{% extends "layouts/base.html" %}
{% members only %}
{% block content %}<h1>Welcome back, {{ user.name }}</h1>{% endblock %}
```

| Tag | Who gets in |
|---|---|
| `{% members only %}` | anyone signed in |
| `{% contributors only %}` | contributors and up |
| `{% editors only %}` | editors and up |
| `{% admins only %}` | admins and owners |
| `{% owners only %}` | owners |
| `{% members only if user.email == "pat@example.com" %}` | signed in **and** the condition holds — any expression over `user`, `record`, … |

The tag works at the top of a page, inside a block, or in a layout — put
`{% members only %}` in `layouts/members.html` and every page that extends it is
members-only. To gate whole folders without touching each page, list them under
[`[access]` in `friendo.toml`](/docs/config#access).

**What a visitor sees.** The same URL, but your `pages/login.html` instead of the
page (status 401 if they're not signed in, 403 if they are but it's not enough).
If you don't have a `login.html`, friendo shows a small built-in one: the site
name, a line about why, and `<friendo-auth>`. Either way, once they sign in the
page reloads and the real content appears. A custom `login.html` gets a `gate`
variable to explain itself:

```html
{# pages/login.html #}
{% extends "layouts/base.html" %}
{% block content %}
  {% if gate.reason == "signin" %}<p>Sign in to see {{ gate.path }}.</p>
  {% elif gate.reason == "role" %}<p>That page is for {{ gate.required }}s and up.</p>
  {% else %}<p>That page isn't available to your account.</p>{% endif %}
  <friendo-auth reload></friendo-auth>
{% endblock %}
```

`reload` on `<friendo-auth>` makes the page reload after signing in or out — use it
on any page that renders `{{ user }}` server-side. Members-only pages are left out
of a [static export](/docs/static-export), since a static host can't check who's
asking.

## Control flow

The usual tags are all there, and they **nest** — a loop inside a loop, an
`if` inside a loop, and so on:

```html
{% for post in collections.blog %}
  <article>
    <h2>{{ post.title }}</h2>
    {% if post.status == "published" %}Live{% elif post.data.featured %}Featured{% else %}Draft{% endif %}
  </article>
{% empty %}
  <p>No posts yet.</p>
{% endfor %}
```

- `{% for x in list %}…{% empty %}…{% endfor %}` — the `{% empty %}` block renders
  when the list is empty.
- `{% if %}…{% elif %}…{% else %}…{% endif %}` — conditions support `and`, `or`,
  `not`, comparisons (`==`, `!=`, `<`, `>`, `<=`, `>=`), and `in`.
- `{% set name = value %}`, `{% include "partial.html" %}`, `{# comments #}`, and
  `{% raw %}…{% endraw %}` (emit template syntax literally).

### Loop variables

Inside a `{% for %}`, `forloop` describes the iteration:

| Variable | Value |
|---|---|
| `forloop.Counter` | 1-based index (1, 2, 3, …) |
| `forloop.Counter0` | 0-based index (0, 1, 2, …) |
| `forloop.Revcounter` | index counting down to 1 |
| `forloop.Revcounter0` | index counting down to 0 |
| `forloop.First` | `true` on the first item |
| `forloop.Last` | `true` on the last item |

## Filters

Filters transform values with `|`. Friendo adds these to the standard Pongo2/Jinja
set (`truncatechars`, `truncatewords`, `upper`, `lower`, `title`, `capfirst`,
`length`, `first`, `last`, `join`, `default`, `striptags`, `urlencode`, `safe`, …):

| Filter | Example | Result |
|---|---|---|
| `asset_url` | `{{ "logo.png"\|asset_url }}` | `/assets/logo.png` |
| `date` | `{{ post.created\|date:"Jan 2, 2006" }}` | a formatted date |
| `resize` | `{{ img\|asset_url\|resize:"300x200" }}` | `/assets/img?w=300&h=200` (a CDN hint) |
| `markdown` | `{{ post.body\|markdown }}` | markdown rendered to HTML |
| `sort_by` | `{% for d in collections.docs\|sort_by:"data.weight" %}` | a list sorted by a (dotted) field |
| `upcoming`, `past`, `in_month`, `on_day` | `{% for e in collections.events\|upcoming %}` | [events](/docs/calendar) by date, repeating ones expanded |
| `google_calendar_url` | `{{ record\|google_calendar_url }}` | Google Calendar's "add this event" link |
| `when` | `{{ record.when\|when }}` | `Sat Oct 4, 10 am – 4 pm` |

### sort_by

`sort_by` orders a list of records by a field — including a nested one like
`data.weight` — numerically when the values are numbers, else alphabetically.
This site's sidebar is built with it:

```html
{% for d in collections.docs|sort_by:"data.weight" %}
  <a href="/docs/{{ d.slug }}">{{ d.title }}</a>
{% endfor %}
```

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
