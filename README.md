# Friendo

> Your site is a folder. Build it locally. Publish it anywhere.

Friendo is a local-first website builder. Your site lives as a directory on your machine — templates, pages, a database, and a single binary that runs the whole thing. No accounts required to build. No lock-in. No magic you can't see.

When you're ready to share it, `friendo deploy` puts it on the internet.

---

## Install

**macOS / Linux — one line:**

```bash
curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | sh
```

This downloads the right prebuilt `friendo` binary for your OS/arch and installs it
to a directory on your PATH.

**Manual download:** grab a binary for your platform from the
[latest release](https://github.com/friendo-world/friendo/releases/latest)
(macOS/Linux `.tar.gz`, Windows `.zip`), extract it, and put `friendo` on your PATH.

**With Go** (installs to `$(go env GOPATH)/bin`):

```bash
go install github.com/friendo-world/friendo/cli/cmd/friendo@latest
```

**From source:** `git clone` this repo, then `npm run build` (builds the admin UI +
compiles the binary to `bin/friendo`).

Check your version with `friendo --version`.

---

## What's in this repo

```
friendo/
├── cli/                # The friendo command (init, serve, push, pull, deploy, export)
├── admin/              # Shared admin UI — one Preact SPA, served by the runtime
├── runtime/
│   └── go/             # The Go runtime — local dev, self-host, and network mode (friendo.world)
└── testsite/           # Example site for development and testing
```

**Docs:** [ARCHITECTURE.md](ARCHITECTURE.md) (how it's designed) ·
[DEVELOPMENT.md](DEVELOPMENT.md) (how to build & run) ·
[ROADMAP.md](ROADMAP.md) (shipped & next).

---

## The pieces

### `friendo` — the CLI

The command-line tool. Init a site, serve it locally, push it to the internet, pull it back down. Thin wrapper around the runtime — it knows how to talk to any running Friendo site via its sync API.

```bash
friendo init my-site        # scaffold a new site
friendo serve               # start the local dev server
friendo build               # compile content/ markdown into the site database
friendo deploy [subdomain]  # publish this folder to a network (friendo.world by default)
friendo push                # push updates to your deployed site
friendo pull --data          # pull remote data into local
friendo export              # export to static HTML
```

### `runtime/go/` — the Go runtime

The core of the project — the one runtime behind everything. A single Go binary that bundles a web server, SQLite database, Pongo2 template engine, admin UI, and sync API. It runs when you type `friendo serve` (one site) and when you self-host; in `friendo network serve` mode the same binary hosts many sites by subdomain — that's how friendo.world runs.

Every Friendo site — local or deployed — exposes the same interface:
- `/` — your site (templates + data)
- `/_/` — admin UI (user management, content editing)
- `/_/api/*` — sync API (push/pull endpoint for templates, data, users)

### `network mode` — friendo.world

friendo.world runs **network mode**: the *same* Go binary in `friendo network serve`, hosting many sites by subdomain in one process, on **Coolify + Hetzner**, with **Cloudflare** as dumb infrastructure (wildcard DNS + TLS/CDN) and **Cloudflare R2** for media. Any self-hoster can run their own network the same way. See [DEPLOY.md](DEPLOY.md).

> friendo previously shipped a Cloudflare Workers ("edge") runtime and a Workers-for-Platforms hosting layer. Both were removed when the project unified on the Go runtime.

---

## Site structure

```
my-site/
├── friendo.toml        # Site config (name, content types)
├── layouts/            # Shared layouts and partials that wrap pages
│   └── base.html
├── pages/              # Page templates — file path maps to URL path
│   ├── index.html
│   └── blog/
│       └── [slug].html
├── content/            # Optional: author content as markdown files
│   └── blog/
│       └── hello.md    # → a record in the "blog" collection
├── assets/             # Static files — CSS, JS, images (served at /assets/)
│   └── style.css
└── data/               # SQLite database
    └── friendo.db
```

Content lives in the database, but you can author it as **markdown files** in
`content/` (Hugo-style): the folder is the collection, YAML front matter sets the
fields, and `friendo serve`/`build`/`push` compile it into the DB.

---

## Content types

Friendo ships with built-in content types — enable the ones your site needs.

| Type | What it's for | Template access |
|---|---|---|
| **Posts** | Blog posts, pages, any authored content | `{{ collections.blog }}` |
| **Comments** | Comments on any post | `{{ record.comments }}` or `<friendo-comments>` |
| **Reactions** | Emoji reactions on posts or comments | `{{ record.reactions }}` or `<friendo-reactions>` |
| **Channels** | Chat rooms, forums, feeds (realtime) | `<friendo-channel>` |
| **Messages** | Messages within a channel | `<friendo-channel>` |
| **Polls** | Polls attached to posts | `{{ record.poll }}` or `<friendo-poll>` |
| **Authors** | People (personas) who create content | `{{ record.author_name }}` |
| **Locations** | Geotag any record | `<friendo-map>` |
| **Files** | Media, uploads, page-bundle galleries | `{{ record.gallery }}` |

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

- **First run:** Create the site **owner** (email + password)
- **Roles:** Capability-based — owner > admin > editor > contributor > member. Contributors edit only their own posts; editors edit any. Owners/admins add users via the admin UI.
- **Presets:** Pick a site type (Personal / Community / Blog) in Settings to set who can post and whether posts need approval.
- **Members:** Visitors sign in passwordlessly with an email code to comment, react, and vote.
- **Sessions:** Bcrypt passwords, token-based sessions stored in the database.
- **Sync:** `friendo push --users` syncs accounts to the deployed site. Bcrypt hashes are portable — same password works everywhere.

Publishing to a network (friendo.world or your own) uses a separate **network account** — passwordless sign-in by email code, with browser device-auth for the CLI (`friendo login`). Hosting sites for others is just the **operator capability** on that account, not a separate password. Network accounts and per-site auth are independent.

---

## Deploy, push, and pull

**`friendo deploy [subdomain]`** — Publish this folder to a network. Defaults to friendo.world; use `--network URL` for your own.

```
$ friendo deploy my-club
  → opens your browser to sign in (first time only)
  → claims my-club.friendo.world
  → pushes your templates, assets, and data
```

Run your own network with `friendo network serve` (see [DEPLOY.md](DEPLOY.md)), or self-host a single site with `friendo serve`.

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

All sync goes through the site's own `/_/api/*` endpoints, authenticated with site admin credentials. The same API works whether your site is on friendo.world, on your own `friendo network`, or self-hosted with `friendo serve`.

---

## Templates

[Pongo2](https://github.com/flosch/pongo2) (Jinja2-compatible). Plain `.html` files with logic.

```html
{% extends "layouts/base.html" %}

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
| **Phase 1.5** | Auth, shared admin SPA + REST API, codebase refactor, working deploy | Complete |
| **Phase 3** | Community features — visitor comments/reactions/polls, realtime channels, locations, media, the `friendo.js` SDK | Complete |
| **v0.2 / v0.3** | Public-safe hardening, then consolidation onto one Go runtime + network mode: server-side community rendering, page-bundle galleries, render/CLI/SDK test coverage, `<friendo-map>`, persona switcher | Complete |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned (0.4+) |

See [ROADMAP.md](ROADMAP.md) for details and known gaps.

---

## Philosophy

**Portability first.** Your site should outlive any tool, platform, or company — including this one.

**Local before cloud.** Building shouldn't require an internet connection or an account.

**Honest templates.** What you see in the editor is what's in the file. Plain text, checkable into git.

**Simple deployment.** One command to publish. The cloud exists to make that easy — not to create dependency.

**Self-hostable.** friendo.world is convenient, not required. The runtime is yours to deploy anywhere.

---

## License

MIT
