---
title: Your first site
slug: first-site
section: Tutorials
weight: 20
description: init, then serve. A running site in a minute, and your first post in five.
---

A friendo site is a folder on your machine. In this tutorial you make one, run
it, write a post, and change how every page looks. You need
[friendo installed](/docs/installation) and a text editor.

## 1. Scaffold

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
├── content/            # posts as markdown files
│   ├── blog/
│   │   └── hello-world.md
│   ├── events/
│   └── groups/
├── assets/
│   └── style.css
└── data/               # made on first run (SQLite)
```

## 2. Serve it

```bash
friendo serve
```

Open **http://localhost:3000**. You'll see the site's name and the *Hello, world*
post. The server watches the folder: edit a file and the browser refreshes.

> **Tip:** port 3000 taken? `friendo serve --port 4000` picks another.

## 3. Write a post

Make a new file, `content/blog/second-post.md`:

```markdown
---
title: My second post
---

Written in a **file**, on my own machine.
```

Save it and look at the home page: the post is there. The folder under `content/`
is the collection (`blog`), the file name is the slug, so the post's own page is
at **http://localhost:3000/blog/second-post**. That page is drawn by
`pages/blog/[slug].html`.

## 4. Change every page at once

Every page extends `layouts/base.html`. Open it and add a footer after `</main>`:

```diff
     <main>
         {% block content %}{% endblock %}
     </main>
+    <footer>Made with friendo.</footer>
     <script src="/friendo.js" defer></script>
```

Save. Every page on the site now has the footer. That's the whole model:
[templates](/docs/templates) render [posts](/docs/posts), and a layout holds
what they share.

## 5. Open the admin

Open **http://localhost:3000/_/**. On your own machine there's nothing to sign in
to: the admin opens and you're the owner. Your two posts are under **blog**. From
here you can write posts without files, change settings, and add people.

Sign-in only appears once the site is somewhere other people can reach. See
[Members, roles and sign-in](/docs/signing-in).

> **What you built.** A site that's a folder: posts in `content/`, pages in
> `pages/`, one layout shared by all of them, and an admin, running on your
> laptop with no account anywhere.

**Next:** [Add sign-in and comments](/docs/add-a-community). And if a word puzzles
you along the way, every one friendo uses is on one page:
[Words friendo uses](/docs/words).
