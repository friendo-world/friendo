---
title: Posts and collections
slug: posts
section: Concepts
weight: 20
description: Everything you write is a post, and posts live in collections.
---

Everything you write in friendo is a **post**, and posts live in **collections**.
A blog is a collection called `blog`; each entry is a post. Events are posts with
a time, groups are posts with members, and `pages` is a collection too. One idea
carries the whole site, so once you know how a blog post works you know how
everything else works.

## Collections

A collection exists as soon as it has a post. Name the ones you mean to keep in
`friendo.toml` and the admin lists them first, in that order:

```toml
[content]
collections = ["blog", "events", "groups", "pages"]
```

Two collections are built in. A post in `events` with a `when` is an
[event](/docs/time): listed by date, in the calendar feed, with RSVPs. Any
collection may hold events; `events` is only the usual home. A post in `groups`
is a [group](/docs/social-graph): it has members, admins and moderators, and
other posts can be filed under it.

The name `profiles` is taken: `pages/profiles/[slug].html` is where a member's
[profile](/docs/profiles) renders, so you can't have a collection called that.

## A post

Every post has the same built-in parts: `id`, `slug`, `title`, `body`, `status`
(`draft`, `pending` or `published`), `published_at`, `created`, `updated`, and an
`author`. Anything else you put on it is a **field**.

```html
<h1>{{ post.title }}</h1>
<p>by {{ post.author.name }}</p>
{{ post.body|markdown }}
<p>Mood: {{ post.fields.mood }}</p>
```

## Fields

A field is any extra key on a post. There are three ways to put one there and they
all land in the same place, `post.fields.<name>`: a line of front matter in a
[content file](/docs/posts-in-files), an input in a [form](/docs/forms) on a
page, or *Add a field* in the [admin](/docs/admin).

You can tell friendo about the fields a collection carries, and the admin lays out
its table and form from them and checks them before saving. Declaring fields
describes them; it doesn't forbid others. The grammar is in
[friendo.toml](/docs/config#fields).

Two keys aren't fields: `when` makes a post an [event](/docs/time) and
`location` gives it a [place](/docs/locations). Friendo lifts them out of the
fields and into the post itself: `post.when`, `post.location`.

## Where posts come from

A post can be written three ways, and they're all the same post afterwards:

- **A file** in `content/`. The folder is the collection, the file name is the slug,
  the front matter is the fields. `friendo serve` reads the folder into the
  database, and the file wins on the next import. These docs are written this way.
  See [Write posts in files](/docs/posts-in-files).
- **The admin** at `/_/`, the same one locally and deployed. See
  [Write posts in the admin](/docs/posts-in-admin).
- **A form on a page**, filled in by a member. See [Let members post from a page](/docs/forms).

## What the community adds to a post

Comments, reactions, a poll, RSVPs, a gallery: friendo attaches these to the post
before your template runs, so they render with no JavaScript. `post.comments` is
the approved comments, `post.reactions` the tallies, `post.poll` the results. The
`<friendo-*>` tags are the interactive spellings of the same things. See
[Server-rendered and live](/docs/two-spellings) for the rule, and
[Template variables](/docs/template-variables#post) for the full list.

## One database everywhere

Every friendo site uses the same tables, so moving posts between sites
(`friendo push --posts`, `friendo pull --posts`) is a copy, not a migration, and a
post renders the same on your laptop, on a server you run, or on friendo.world.
