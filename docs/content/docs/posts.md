---
title: Posts & collections
slug: posts
section: Concepts
weight: 5
---

Everything you write in friendo is a **post**, and posts live in **collections**.
A blog is a collection called `blog`; each entry is a post. Events are posts with
a time, groups are posts with members, and `pages` is a collection too.

## Collections

A collection exists as soon as it has a post. Name the ones you mean to keep in
`friendo.toml` and the admin lists them first, in that order:

```toml
[content]
collections = ["blog", "events", "groups", "pages"]
```

Two collections are built in:

| Collection | What makes it special |
|---|---|
| `events` | A post with a `when` is an [event](/docs/calendar): listed by date, in the calendar feed, with RSVPs. Any collection may hold events; `events` is just the usual home. |
| `groups` | A post here is a [group](/docs/groups): it has members, admins and moderators, and other posts can be filed under it. |

The name `profiles` is taken: `pages/profiles/[slug].html` is where a member's
[profile](/docs/signing-in#profiles) renders, so you can't have a collection called that.

## A post

Every post has the same built-in parts: `id`, `slug`, `title`, `body`, `status`
(`draft`, `pending` or `published`), `published_at`, `created`, `updated`, and an
`author`. Anything else you put on it is a **field**.

```html
<h1>{{ post.title }}</h1>
<p>by {{ post.author.name }} · {{ post.created|date:"Jan 2, 2006" }}</p>
{{ post.body|markdown }}
<p>Mood: {{ post.fields.mood }}</p>
```

## Fields

A field is any extra key on a post. There are three ways to put one there and they
all land in the same place, `post.fields.<name>`:

- **Front matter** in a content file: `mood: calm`.
- **A form** on a page: `<input name="mood">` in a [`<friendo-form>`](/docs/community#posting-from-a-page).
- **The admin**: *Add a field* on any post.

Tell friendo about the fields a collection carries and the admin lays out its
table and form from them, and checks them before saving:

```toml
[content.blog.fields]
tags   = "tags"
cover  = "image"
weight = "number"
mood   = { kind = "text", choices = ["calm", "wild"], required = true, hint = "How the post feels" }
```

A bare string is the field's **kind**: `text`, `paragraph`, `number`, `checkbox`,
`tags`, `image` or `json`. A table adds `choices`, `required` and a `hint`. Declaring
fields describes them; it doesn't forbid others. See [friendo.toml](/docs/config#fields).

Two keys aren't fields: `when` makes a post an [event](/docs/calendar) and
`location` gives it a [place](/docs/locations). Friendo lifts them out of the
fields and into the post itself: `post.when`, `post.location`.

## Writing posts in files

Drop markdown files in `content/` and friendo reads them into the database. The
folder is the collection, the file name is the slug:

```
content/
  blog/
    hello-world.md      → blog, slug "hello-world"
  events/
    harvest-fair.md     → events, slug "harvest-fair"
```

```markdown
---
title: Hello, world
tags: [intro, welcome]
weight: 1
---

Your **markdown** body. Render it with the `markdown` filter.
```

`title`, `slug`, `status` and `date` are the post's own parts; every other key is a
field. `friendo serve` reads `content/` when it starts and again on every edit;
`friendo import` does it on demand; `push` and `deploy` do it before uploading.
A post that came from a file says so in the admin, because the file wins on the
next import. This documentation is written exactly this way.

### A post as a folder

A post can be a folder with an `index.md` inside. Images sitting next to it become
the post's **gallery**, no upload step:

```
content/blog/my-trip/
  index.md          → blog / my-trip
  01-sunrise.jpg    ┐  post.gallery
  02-harbor.jpg     ┘
```

```html
{% for img in post.gallery %}<img src="{{ img.url }}" alt="">{% endfor %}
```

The images are copied under `assets/galleries/` on each import and travel with
`friendo push --posts`. `.png`, `.jpg`, `.gif`, `.webp` and `.svg` are accepted.

## Writing posts in the admin

Every site has an admin at `/_/` (the same one locally and deployed). Pick a
collection, and its posts show as a table whose columns are the fields they carry.
Opening a post edits every part and every field, plus its **When** and **Location**.

## What the community adds to a post

Comments, reactions, a poll, RSVPs, a gallery: friendo attaches these to the post
before your template runs, so they render with no JavaScript. `post.comments` is
the approved comments, `post.reactions` the tallies, `post.poll` the results. The
[`<friendo-*>` tags](/docs/community) are the interactive versions of the same
things. See [Templates](/docs/templates#what-a-post-carries).

## One database everywhere

Every friendo site uses the same tables, so moving posts between sites
(`friendo push --posts`, `friendo pull --posts`) is a copy, not a migration, and a
post renders the same on your laptop, on a server you run, or on friendo.world.
