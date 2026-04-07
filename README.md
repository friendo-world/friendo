# Friendo

> Your site is a folder. Build it locally. Publish it anywhere.

Friendo is a local-first website builder built around the idea that you should fully own what you make. No accounts required to build. No lock-in. No magic you can't see. Your site lives as a directory on your machine — templates, pages, a database, and a single binary that runs the whole thing.

When you're ready to share it with the world, `friendo deploy` puts it on the internet in seconds.

---

## What's in this repo

This is the Friendo monorepo. It contains three projects that together form the full Friendo ecosystem.

```
friendo/
├── binary/       # The Friendo runtime — Go binary, PocketBase, Pongo2 SSR
├── editor/       # The Friendo desktop editor — local-first WYSIWYG site builder
└── world/        # Friendo.world — the hosted publishing destination
```

Each package has its own README with setup and contribution instructions.

---

## The three parts

### `friendo` — the binary

The core of the project. A single Go binary that bundles a web server, a database (via PocketBase), and a template renderer (via Pongo2). Start a site with `friendo init`, build it with `friendo serve`, and ship it with `friendo deploy`.

The binary is the source of truth. Everything else in this project is built on top of it.

**Key ideas:**
- Your site is a directory. Templates, pages, assets, and data all live as plain files.
- The database is SQLite via PocketBase — portable, inspectable, no server required.
- Templates use Pongo2, a Go implementation of the Django/Jinja2 template language. If you've used Jinja, Nunjucks, Twig, or Liquid, you already know it.
- Zero runtime dependencies. One binary, cross-compiled for macOS, Linux, and Windows.

### `friendo editor` — the desktop app

A local-first desktop application for building Friendo sites visually. The editor talks to a running Friendo binary over localhost — it's a GUI for the binary, not a replacement for it. The binary remains the source of truth.

The editor offers a WYSIWYG block-style editing experience. Every block is honest: you can always drop into the underlying Pongo2 template and edit it directly. No proprietary markup hiding underneath.

> The editor is planned for Phase 2. See the roadmap below.

### `friendo.world` — the publishing platform

A destination for publishing Friendo sites. Run `friendo deploy` and your site appears on a `yourname.friendo.world` subdomain within seconds. Bring your own domain if you prefer.

Friendo.world is the easiest place to publish, but not the only one. You can deploy to your own Cloudflare account, serve the binary yourself on any VPS, or export a static snapshot and put it anywhere.

The platform is built on Cloudflare Workers, D1, and R2 — so your site runs at the edge, globally, for almost nothing.

> Friendo.world is part of Phase 1. See the roadmap below.

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
├── friendo.toml        # Site config (name, collections, deploy settings)
├── templates/          # Base layouts and reusable template partials
│   └── base.html
├── pages/              # Page templates — file path maps to URL path
│   ├── index.html
│   └── blog/
│       └── [slug].html
├── public/             # Static assets (CSS, JS, images) — served as-is
│   └── style.css
└── data/               # PocketBase SQLite database lives here
    └── friendo.db
```

---

## Templates

Friendo uses [Pongo2](https://github.com/flosch/pongo2), a Go implementation of the Django/Jinja2 template language. Templates are plain `.html` files with logic sprinkled in.

```html
{% extends "templates/base.html" %}

{% block content %}
  <h1>{{ site.name }}</h1>
  {% for post in collections.posts %}
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
| `resize` | `{{ image\|resize:"800x600" }}` | Resize an uploaded image |
| `asset_url` | `{{ file\|asset_url }}` | Resolve a PocketBase file to its URL |
| `date` | `{{ post.created\|date:"Jan 2006" }}` | Format a date |

---

## The deploy story

When you run `friendo deploy`, the following happens:

1. Your PocketBase schema is exported and migrated to Cloudflare D1
2. Uploaded files and assets are synced to Cloudflare R2
3. Your templates are bundled into a Cloudflare Worker (rendered via Nunjucks, which is syntax-compatible with Pongo2)
4. The Worker is deployed and assigned your `friendo.world` subdomain

Your site runs at the edge. The behavior in production matches local exactly — same template syntax, same routing, same data.

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

---

## Contributing

Friendo is in early development. See [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines. Each package (`binary/`, `editor/`, `world/`) has its own setup instructions and issue tracker labels.

---

## License

MIT
