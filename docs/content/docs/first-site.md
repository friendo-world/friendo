---
title: Your first site
slug: first-site
section: Getting started
weight: 2
---

A friendo site is a folder on your machine. Let's make one, run it, and edit it.

## Scaffold

```bash
friendo init my-site
cd my-site
```

This makes a small, complete site:

```
my-site/
├── friendo.toml        # site config
├── layouts/
│   └── base.html       # shared layout
├── pages/              # each file maps to a URL
│   ├── index.html
│   ├── 404.html
│   ├── inbox.html      # members only: what happened to you
│   ├── blog/
│   │   └── [slug].html # one page per blog post
│   ├── events/         # the calendar: a list and an event page
│   ├── groups/         # groups: a list and a group page
│   └── profiles/
│       └── [slug].html # a member's profile page
├── content/            # optional: posts as markdown files
│   ├── blog/
│   │   └── hello-world.md
│   ├── events/
│   └── groups/
├── assets/
│   └── style.css
└── data/               # made on first run (SQLite)
```

## Serve it

```bash
friendo serve
```

Your site is now at **http://localhost:3000**, with hot reload: edit a template
and the browser refreshes.

| URL | What |
|---|---|
| `http://localhost:3000` | Your site |
| `http://localhost:3000/_/` | The admin: posts, review, members, settings |

## Open the admin

Open `http://localhost:3000/_/`. On your own machine there's nothing to sign in
to; the admin just opens, and you're the owner. From here you can write posts,
manage collections, and add people.

(Sign-in only appears once the site is somewhere other people can reach: on a
server, you'll be asked to confirm your email with a code the first time. See
[Signing in & roles](/docs/signing-in).)

## Edit a page

Pages live in `pages/` and are plain templates. Open `pages/index.html`:

```html
{% extends "layouts/base.html" %}

{% block content %}
  <h1>{{ site.name }}</h1>
  {% for post in collections.blog %}
    <article>
      <h2>{{ post.title }}</h2>
      <a href="/blog/{{ post.slug }}">Read more</a>
    </article>
  {% endfor %}
{% endblock %}
```

That's the whole model: [templates](/docs/templates) render
[posts](/docs/posts). When you're ready to share it, [deploy](/docs/deploy). And
if a word puzzles you along the way, every one friendo uses is on one page:
[Words friendo uses](/docs/words).
