---
title: Content & collections
slug: content
group: Concepts
weight: 5
---

Content in Friendo lives in your site's database as **records**, grouped into
**collections**. A blog is a collection called `blog`; its posts are records.

## Built-in content types

Friendo ships with content types that work identically on your machine and in the
cloud. Enable the ones you need in [`friendo.toml`](/docs/config):

```toml
[content]
types = ["posts", "comments", "reactions"]
```

| Type | What it's for | In templates |
|---|---|---|
| **Posts** | Blog posts, pages, any authored content | `{{ collections.blog }}` |
| **Comments** | Comments on a post | `{{ record.comments }}` (approved) or `<friendo-comments>` |
| **Reactions** | Emoji reactions on posts or comments | `{{ record.reactions }}` or `<friendo-reactions>` |
| **Channels** | Chat rooms, forums, feeds (realtime) | `<friendo-channel>` |
| **Messages** | Messages within a channel | `<friendo-channel>` |
| **Polls** | Polls attached to a post | `{{ record.poll }}` or `<friendo-poll>` |
| **Authors** | People (personas) who create content | `{{ record.author_name }}` |
| **Locations** | Geotag any record | `<friendo-map>` |
| **Events** | A post with a `when` — listed by date, subscribable at `/calendar.ics` | `{{ record.when }}`, `collections.events\|upcoming` |
| **Files** | Media and uploads (incl. galleries) | `{{ record.gallery }}` |

Community relations (`record.comments` / `.reactions` / `.poll` / `.gallery`) render
**server-side** on a post's page; the `<friendo-*>` components are the interactive,
signed-in equivalents. See [Templates](/docs/templates#community-relations) and
[Community features](/docs/community).

## Collections in templates

Any collection is available under `collections`:

```html
{% for post in collections.blog %}
  <h2>{{ post.title }}</h2>
  <p>{{ post.body|truncatechars:200 }}</p>
{% endfor %}
```

A record has fields like `id`, `slug`, `title`, `body`, `status`, `created`, and
`published_at`.

## Editing content

Every Friendo site has an admin UI at `/_/` for creating and editing records —
the same UI locally and when deployed. It's a content editor, user management,
and settings, backed by the site's REST API.

## Authoring in files (content/)

You can build your whole site from markdown files. Drop them in a `content/`
folder and Friendo compiles them into records:

```
content/
  blog/
    hello-world.md      → blog collection, slug "hello-world"
  docs/
    installation.md     → docs collection, slug "installation"
```

The **folder under `content/` is the collection**; the filename is the slug. Each
file has YAML front matter. `title`, `slug`, `status` and `date` are the record's
own fields; `location` becomes a [map pin](/docs/maps); `when` (with `ends`,
`timezone`, `repeats`, `except`) makes the post a [calendar event](/docs/calendar);
and any other key becomes the record's `data`, readable in templates as
`record.data.<field>`:

```markdown
---
title: Hello, world
slug: hello-world
tags: [intro, welcome]
weight: 1
---

Your **markdown** body. Rendered with the `markdown` filter at template time.
```

`friendo serve` compiles `content/` on startup and re-imports on every edit
(hot reload); `friendo build` does it on demand; and `friendo push`/`deploy`
compile before uploading. Importing upserts by `(collection, slug)`, so `content/`
is the source of truth — this documentation site is authored exactly this way.

### Page bundles & galleries

A post can be a **folder** instead of a single file: put an `index.md` in
`content/<collection>/<slug>/` and the folder name becomes the slug. Any images
sitting next to it are imported as the post's **gallery** — no upload step:

```
content/blog/my-trip/
  index.md          → blog / my-trip
  01-sunrise.jpg    ┐  attached to the post as record.gallery
  02-harbor.jpg     ┘  (served from /assets/, and synced on deploy)
```

Render them in the post template:

```html
{% for img in record.gallery %}
  <img src="{{ img.url }}" alt="">
{% endfor %}
```

The images are copied under `assets/galleries/…` (regenerated on every build, so
`content/` stays the source of truth) and ride along on `friendo push --data` like
any uploaded media. Supported: `.png`, `.jpg`, `.gif`, `.webp`, `.svg`.

This site's sidebar, for instance, is generated from its docs collection sorted by
each file's `weight`:

```html
{% for d in collections.docs|sort_by:"data.weight" %}
  <a href="/docs/{{ d.slug }}">{{ d.title }}</a>
{% endfor %}
```

## One schema, everywhere

Every friendo site uses the exact same table definitions, so moving data between
sites (`friendo push --data`, `friendo pull --data`) is a copy, not a migration —
the same content renders the same way locally, on a server you run, or on
friendo.world.
