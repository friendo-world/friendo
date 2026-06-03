# Friendo

> Your site is a folder. Build it locally. Publish it anywhere.

Friendo is a local-first website builder. Your site lives as a directory on your machine — templates, pages, a database, and a single binary that runs the whole thing. No accounts required to build. No lock-in. No magic you can't see.

When you're ready to share it, `friendo deploy` puts it on the internet.

---

## What's in this repo

```
friendo/
├── cli/                # The friendo command (init, serve, push, pull, deploy, export)
├── admin/              # Shared admin UI — one Preact SPA, served by both runtimes
├── runtime/
│   ├── go/             # Go site runtime (local dev, self-hosted VPS)
│   └── edge/           # JS site runtime (Cloudflare Workers, self-hostable)
├── platform/           # friendo.world (managed hosting, wraps runtime/edge/)
└── testsite/           # Example site for development and testing
```

**Docs:** [ARCHITECTURE.md](ARCHITECTURE.md) (how it's designed) ·
[DEVELOPMENT.md](DEVELOPMENT.md) (how to build & run) ·
[ROADMAP.md](ROADMAP.md) (shipped & next).

---

## The pieces

### `friendo` — the CLI

The command-line tool. Init a site, serve it locally, push it to the internet, pull it back down. Thin wrapper around the runtimes — it knows how to talk to any running Friendo site via its sync API.

```bash
friendo init my-site        # scaffold a new site
friendo serve               # start the local dev server
friendo deploy              # first-time deploy (interactive)
friendo push                # push updates to your deployed site
friendo pull --data          # pull remote data into local
friendo export              # export to static HTML
```

### `runtime/go/` — the Go runtime

The core of the project. A single Go binary that bundles a web server, SQLite database, Pongo2 template engine, admin UI, and sync API. This is what runs when you type `friendo serve`, and it's what you'd run on a VPS for self-hosting.

Every Friendo site — local or deployed — exposes the same interface:
- `/` — your site (templates + data)
- `/_/` — admin UI (user management, content editing)
- `/_/api/*` — sync API (push/pull endpoint for templates, data, users)

### `runtime/edge/` — the JS runtime

The same site runtime, built for Cloudflare Workers. A single-site Hono app with a lightweight Jinja2-compatible template engine (no `eval()`, Workers-safe), D1 for data, R2 for assets. Same admin UI, same sync API, same auth.

This is what self-hosters deploy to their own Cloudflare account.

### `platform/` — friendo.world

The managed hosting layer, built on [Cloudflare Workers for Platforms](https://developers.cloudflare.com/cloudflare-for-platforms/workers-for-platforms/). A dispatch Worker that routes by subdomain, serves the platform UI (landing page, dashboard), handles platform auth, and provisions new sites.

Each site on friendo.world runs as its own isolated user Worker — the exact same `runtime/edge/` code that self-hosters deploy — with its own D1 database and R2 bucket. The platform provisions these resources automatically when you run `friendo deploy`.

The platform is optional. Everything it does, the runtimes can do on their own.

---

## Site structure

```
my-site/
├── friendo.toml        # Site config (name, content types)
├── templates/          # Base layouts and partials
│   └── base.html
├── pages/              # Page templates — file path maps to URL path
│   ├── index.html
│   └── blog/
│       └── [slug].html
├── public/             # Static assets (CSS, JS, images)
│   └── style.css
└── data/               # SQLite database
    └── friendo.db
```

---

## Content types

Friendo ships with built-in content types that work identically on your machine and in the cloud.

| Type | What it's for | Template access |
|---|---|---|
| **Posts** | Blog posts, pages, any authored content | `{{ collections.blog }}` |
| **Comments** | Threaded comments on any post | `{{ post.comments }}` |
| **Reactions** | Emoji reactions on posts or comments | `{{ post.reactions }}` |
| **Channels** | Chat rooms, forums, feeds | `{{ channels }}` |
| **Messages** | Messages within a channel | `{{ channel.messages }}` |
| **Polls** | Polls attached to posts | `{{ post.poll }}` |
| **Authors** | People who create content | `{{ authors }}` |
| **Locations** | Geotag any record | `{{ post.location }}` |
| **Files** | Media and uploads | `{{ post.files }}` |

Enable what you need in `friendo.toml`:

```toml
[site]
name = "My Site"

[content]
types = ["posts", "comments", "reactions"]
```

---

## Auth

Every Friendo site — local or deployed — has the same auth model:

- **First run:** Create a superadmin account (email + password)
- **Users:** Superadmin can add users via the admin UI with roles (admin, editor, member)
- **Sessions:** Bcrypt passwords, token-based sessions stored in the database
- **Sync:** `friendo push --users` syncs accounts to the deployed site. Bcrypt hashes are portable — same password works everywhere.

The friendo.world platform has its own separate auth for managing your platform account and deployed sites. Site auth and platform auth are independent.

---

## Deploy, push, and pull

**`friendo deploy`** — First-time setup. Interactive wizard that provisions your site on a hosting target.

```
$ friendo deploy

Where do you want to deploy?
  1. friendo.world (managed hosting)
  2. Cloudflare Workers (your own account)
  3. VPS / self-hosted server
```

Saves the target URL in `friendo.toml`. You only run this once.

**`friendo push`** — Push local changes to your deployed site.

```bash
friendo push                # templates + assets
friendo push --data         # also sync records
friendo push --users        # also sync user accounts
```

**`friendo pull`** — Pull remote data into your local database.

```bash
friendo pull --data         # pull records
friendo pull --users        # pull user accounts
```

All sync goes through the site's own `/_/api/*` endpoints, authenticated with site admin credentials. The same API works whether your site is on friendo.world, your own Cloudflare account, or a VPS.

---

## Templates

[Pongo2](https://github.com/flosch/pongo2) (Jinja2-compatible). Plain `.html` files with logic.

```html
{% extends "templates/base.html" %}

{% block content %}
  <h1>{{ site.name }}</h1>
  {% for post in collections.blog %}
    <article>
      <h2>{{ post.title }}</h2>
      <p>{{ post.body|truncate:200 }}</p>
      <a href="/blog/{{ post.slug }}">Read more</a>
    </article>
  {% endfor %}
{% endblock %}
```

---

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **Phase 1** | CLI + Go runtime + friendo.world foundation | Complete |
| **Phase 1.5** | Auth, shared admin SPA + REST API, codebase refactor, Workers for Platforms, working deploy | Complete |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned |
| **Phase 3** | Community features (visitor comments/reactions/polls), template marketplace | Planned |

See [ROADMAP.md](ROADMAP.md) for details and known gaps.

---

## Philosophy

**Portability first.** Your site should outlive any tool, platform, or company — including this one.

**Local before cloud.** Building shouldn't require an internet connection or an account.

**Honest templates.** What you see in the editor is what's in the file. Plain text, checkable into git.

**Simple deployment.** One command to publish. The cloud exists to make that easy — not to create dependency.

**Self-hostable.** friendo.world is convenient, not required. The runtimes are yours to deploy anywhere.

---

## License

MIT
