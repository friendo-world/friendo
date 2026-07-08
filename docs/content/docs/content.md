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
| **Comments** | Threaded comments on a post | `{{ post.comments }}` |
| **Reactions** | Emoji reactions on posts or comments | `{{ post.reactions }}` |
| **Channels** | Chat rooms, forums, feeds | `{{ channels }}` |
| **Messages** | Messages within a channel | `{{ channel.messages }}` |
| **Polls** | Polls attached to a post | `{{ post.poll }}` |
| **Authors** | People who create content | `{{ authors }}` |
| **Locations** | Geotag any record | `{{ post.location }}` |
| **Files** | Media and uploads | `{{ post.files }}` |

## Collections in templates

Any collection is available under `collections`:

```html
{% for post in collections.blog %}
  <h2>{{ post.title }}</h2>
  <p>{{ post.body|truncate:200 }}</p>
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
file has YAML front matter, and any keys beyond `title`/`slug`/`status`/`date`
become the record's `data`, readable in templates as `record.data.<field>`:

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

This site's sidebar, for instance, is generated from its docs collection sorted by
each file's `weight`:

```html
{% for d in collections.docs|sort_by:"data.weight" %}
  <a href="/docs/{{ d.slug }}">{{ d.title }}</a>
{% endfor %}
```

## Common schema, everywhere

Both runtimes use the exact same table definitions, so moving data between them
is a copy, not a migration — the same content renders the same way locally,
on your own Cloudflare account, or on friendo.world.
