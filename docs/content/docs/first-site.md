---
title: Your first site
slug: first-site
group: Getting started
weight: 2
---

A Friendo site is a folder on your machine. Let's make one, run it, and edit it.

## Scaffold

```bash
friendo init my-site
cd my-site
```

This creates a small, complete site:

```
my-site/
├── friendo.toml        # site config
├── layouts/
│   └── base.html       # shared layout
├── pages/              # each file maps to a URL
│   ├── index.html
│   ├── 404.html
│   └── blog/
│       └── [slug].html # dynamic route for a single blog post
├── assets/
│   └── style.css
└── data/               # created on first run (SQLite)
```

## Serve it

```bash
friendo serve
```

Your site is now at **http://localhost:3000**, with hot reload — edit a template
and the browser refreshes.

| URL | What |
|---|---|
| `http://localhost:3000` | Your site |
| `http://localhost:3000/_/` | The admin UI (content + users) |

## Create the admin account

Open `http://localhost:3000/_/` and create the first account (email + password).
This is your **superadmin** — see [Auth & users](/docs/auth). From the admin UI
you can write posts, manage collections, and add users.

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
[content](/docs/content). When you're ready to share it, [deploy](/docs/deploy).
