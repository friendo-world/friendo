# Friendo

> Your site is a folder. Build it locally. Publish it anywhere.

Friendo is a local-first website builder built around the idea that you should fully own what you make. No accounts required to build. No lock-in. No magic you can't see. Your site lives as a directory on your machine — templates, pages, a database, and a single binary that runs the whole thing.

When you're ready to share it with the world, `friendo deploy` puts it on the internet in seconds.

---

## What's in this repo

This is the Friendo monorepo. It contains three projects that together form the full Friendo ecosystem.

```
friendo/
├── binary/       # The Friendo runtime — Go binary, SQLite, Pongo2 SSR
├── world/        # Friendo.world — Cloudflare Worker, D1, R2, Better Auth
├── editor/       # The Friendo desktop editor — local-first WYSIWYG site builder (Phase 2)
└── testsite/     # Example site for development and testing
```

Each package has its own README with setup and contribution instructions.

---

## The three parts

### `friendo` — the binary

The core of the project. A single Go binary that bundles a web server, an embedded SQLite database, and a template renderer (via Pongo2). Start a site with `friendo init`, build it with `friendo serve`, and ship it with `friendo deploy`.

The binary is the source of truth. Everything else in this project is built on top of it.

**Key ideas:**
- Your site is a directory. Templates, pages, assets, and data all live as plain files.
- The database is embedded SQLite — portable, inspectable, no server required.
- Templates use Pongo2, a Go implementation of the Django/Jinja2 template language. If you've used Jinja, Nunjucks, Twig, or Liquid, you already know it.
- Zero runtime dependencies. One binary, cross-compiled for macOS, Linux, and Windows.

### `friendo editor` — the desktop app

A local-first desktop application for building Friendo sites visually. The editor talks to a running Friendo binary over localhost — it's a GUI for the binary, not a replacement for it. The binary remains the source of truth.

The editor offers a WYSIWYG block-style editing experience. Every block is honest: you can always drop into the underlying Pongo2 template and edit it directly. No proprietary markup hiding underneath.

> The editor is planned for Phase 2. See the roadmap below.

### `friendo.world` — the publishing platform

A destination for publishing Friendo sites. Run `friendo deploy` and your site appears on a `yourname.friendo.world` subdomain within seconds. Bring your own domain if you prefer.

Friendo.world is the easiest place to publish, but not the only one. You can deploy to your own Cloudflare account, serve the binary yourself on any VPS, or export a static snapshot and put it anywhere.

The platform is a single Cloudflare Worker that serves all Friendo sites via subdomain routing, backed by D1 (shared database) and R2 (templates and assets). Authentication uses [Better Auth](https://better-auth.com/) with a browser-based device flow — `friendo deploy` opens your browser to sign in, then the CLI picks up the session automatically.

> Friendo.world is part of Phase 1. See the roadmap below.

---

## Content types

Friendo ships with a set of built-in content types that cover most of what people build on the web. Rather than letting you define arbitrary database schemas, Friendo provides opinionated, well-structured tables that work identically on your local machine and in the cloud.

You enable the content types your site needs in `friendo.toml`:

```toml
[site]
name = "My Site"

[content]
types = ["posts", "comments", "reactions"]
```

### Available content types

| Type | What it's for | Template access |
|---|---|---|
| **Posts** | Blog posts, pages, recipes, any authored content | `{{ collections.blog }}`, `{{ collections.pages }}` |
| **Comments** | Threaded comments on any post | `{{ post.comments }}` |
| **Reactions** | Emoji reactions on posts or comments | `{{ post.reactions }}` |
| **Channels** | Chat rooms, forums, feeds | `{{ channels }}` |
| **Messages** | Messages within a channel (threaded) | `{{ channel.messages }}` |
| **Polls** | Polls attached to posts | `{{ post.poll }}` |
| **Authors** | People who create content on the site | `{{ authors }}` |
| **Locations** | Geotag any record (posts, events, authors) | `{{ post.location }}` |
| **Files** | Media and uploads attached to any record | `{{ post.files }}` |

Content types are designed to be expanded over time. Future releases will add types for events/calendars, matchmaking profiles, commerce, and more — without breaking existing sites.

---

## Auth

Auth works differently in each context:

**Local admin UI (`/_/`)** — Lightweight. A first-run password stored hashed in the local database. No OAuth, no sessions infrastructure. It's your machine. Pass `--open-admin` to skip auth entirely during development.

**Deploy (`friendo deploy`)** — Browser-based device flow. The CLI opens your browser to `friendo.world/cli/auth`, where you sign in with email + password via Better Auth. The session token is stored in `~/.friendo/config` and reused for subsequent deploys.

**Site visitors on friendo.world** — Anonymous-first. Visitors can comment, react, and participate with just a display name. They can optionally upgrade to a persistent identity via email. This matches the "no accounts required" philosophy — participating on a Friendo site shouldn't require a third-party login.

The `authors` table stores site-level identities. Deploy auth uses Better Auth's `user`/`session`/`account` tables, managed entirely by the friendo.world Worker — the local binary never handles production auth.

---

## CLI reference

```bash
friendo init [name]     # Scaffold a new site directory
friendo serve           # Start the local dev server (with hot reload)
friendo deploy          # Deploy to Friendo.world
friendo export          # Export a portable bundle (static or self-contained)
```

---

## Site structure

A Friendo site looks like this:

```
my-site/
├── friendo.toml        # Site config (name, content types, deploy settings)
├── templates/          # Base layouts and reusable template partials
│   └── base.html
├── pages/              # Page templates — file path maps to URL path
│   ├── index.html
│   └── blog/
│       └── [slug].html
├── public/             # Static assets (CSS, JS, images) — served as-is
│   └── style.css
└── data/               # SQLite database lives here
    └── friendo.db
```

---

## Templates

Friendo uses [Pongo2](https://github.com/flosch/pongo2), a Go implementation of the Django/Jinja2 template language. Templates are plain `.html` files with logic sprinkled in.

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

Friendo extends Pongo2 with a set of built-in filters for common tasks:

| Filter | Example | Description |
|---|---|---|
| `resize` | `{{ image|resize:"800x600" }}` | Resize an uploaded image |
| `asset_url` | `{{ file|asset_url }}` | Resolve a file to its URL |
| `date` | `{{ post.created|date:"Jan 2006" }}` | Format a date |

---

## The deploy story

When you run `friendo deploy`, the following happens:

1. Your browser opens to sign in (or sign up) on friendo.world
2. Your subdomain is reserved (e.g. `my-site.friendo.world`)
3. Your content (posts, comments, authors, etc.) is synced to friendo.world's shared D1 database
4. Your templates and static assets are uploaded to Cloudflare R2
5. Your site is live — served by a Cloudflare Worker with a lightweight Jinja2-compatible engine that matches Pongo2 locally

The Worker is already running and serves all Friendo sites via subdomain routing. Deploy doesn't upload or modify the Worker — it only syncs your data and templates.

Because all Friendo sites share a common schema, deploy is a data sync — not a schema migration. This keeps deploys fast, safe, and idempotent.

---

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **Phase 1** | Friendo binary + CLI + Friendo.world deploy | In progress |
| **Phase 2** | Friendo desktop editor (Tauri-based, WYSIWYG) | Planned |
| **Phase 3** | Friendo.world community features, template marketplace | Planned |

---

## Philosophy

**Portability first.** Your site should outlive any tool, platform, or company — including this one. If Friendo disappeared tomorrow, your site directory would still work with the binary you already have.

**Local before cloud.** Building a site shouldn't require an internet connection, an account, or a browser tab. Friendo is a desktop-class tool for a desktop-class workflow.

**Honest templates.** What you see in the editor is what's in the file. Pongo2 templates are plain text. Open them in any editor. Check them into git. Grep them. They're yours.

**Simple deployment.** Publishing to the web should be one command. The cloud layer exists to make that true — not to create dependency.

**Opinionated structure.** Friendo doesn't let you define arbitrary schemas. It ships content types that cover what people actually build — blogs, forums, chat, comments, polls, geo — and makes them work seamlessly from local to cloud. This constraint is what makes one-command deploy possible.

---

## Contributing

Friendo is in early development. See [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines. Each package (`binary/`, `editor/`, `world/`) has its own setup instructions and issue tracker labels.

---

## License

MIT
