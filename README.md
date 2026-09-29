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
├── sdk/                # friendo.js — the <friendo-*> Web Components
├── runtime/
│   └── go/             # The Go runtime — local dev, self-host, and network mode (friendo.world)
├── docs/               # docs.friendo.world — itself a friendo site
├── www/                # friendo.world's landing page — the network's home site
└── testsite/           # Example site for development and testing
```

**Docs:** [docs.friendo.world](https://docs.friendo.world) (building a site) ·
[ARCHITECTURE.md](ARCHITECTURE.md) (how it's designed) ·
[DEVELOPMENT.md](DEVELOPMENT.md) (how to build & run) ·
[DEPLOY.md](DEPLOY.md) (hosting a network) ·
[ROADMAP.md](ROADMAP.md) (shipped & next).

---

## The pieces

### `friendo` — the CLI

The command-line tool. Init a site, serve it locally, push it to the internet, pull it back down. Thin wrapper around the runtime — it knows how to talk to any running Friendo site via its sync API.

```bash
friendo init my-site        # scaffold a new site
friendo serve               # start the local dev server
friendo import              # read content/ markdown into the site database
friendo deploy [subdomain]  # publish this folder to a network (friendo.world by default)
friendo open-admin          # open your deployed site's admin, signed in
friendo push                # push updates to your deployed site
friendo pull --posts        # pull remote posts into local
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
├── friendo.toml        # Site config (name, collections)
├── layouts/            # Shared layouts and partials that wrap pages
│   └── base.html
├── pages/              # Page templates — file path maps to URL path
│   ├── index.html
│   └── blog/
│       └── [slug].html
├── content/            # Optional: author content as markdown files
│   └── blog/
│       └── hello.md    # → a post in the "blog" collection
├── assets/             # Static files — CSS, JS, images (served at /assets/)
│   └── style.css
└── data/               # SQLite database
    └── friendo.db
```

Content lives in the database, but you can author it as **markdown files** in
`content/` (Hugo-style): the folder is the collection, YAML front matter sets the
fields, and `friendo serve`/`import`/`push` read it into the DB.

---

## Posts and features

Everything you write is a **post** in a **collection** (`blog`, `events`, `groups`,
`pages`). Community **features** are switches; each has a tag and, where a no-JS
read helps, a spelling on the post itself.

| Feature | What it's for | In templates |
|---|---|---|
| **Comments** | Comments on any post | `{{ post.comments }}` or `<friendo-comments>` |
| **Reactions** | Emoji reactions on posts or comments | `{{ post.reactions }}` or `<friendo-reactions>` |
| **Chats** | Chat rooms, live threads (realtime) | `<friendo-chat>` |
| **Polls** | Polls attached to posts | `{{ post.poll }}` or `<friendo-poll>` |
| **Profiles** | A member's public face, at `/profiles/<slug>` | `{{ post.author.name }}`, `<friendo-profile>` |
| **Locations** | A place on a post | `{{ post.location }}`, `<friendo-map>` |
| **Calendar** | An event is a post with a `when` | `{{ event.when }}`, `<friendo-calendar>`, `<friendo-rsvp>` |
| **Follows**, **Groups** | Who follows whom; groups with members | `<friendo-follow>`, `<friendo-group>` |
| **Galleries** | Images beside a post's markdown file | `{{ post.gallery }}` |

On a network's own domain, three more tags talk to the network rather than a site: `<friendo-account>` (your sites, domains, Open admin), `<friendo-console>` (the operator's levers) and `<friendo-activate>` (linking `friendo login`). The network serves each on a built-in page; a **home site** can wrap the same tag in its own design.

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

- **Sign-in is a code sent to your email** — for every role. Passwords are opt-in per site (Settings → *Allow signing in with a password*).
- **On your laptop** `friendo serve` opens the admin with no sign-in at all (only for requests from the same machine, with no email provider configured). Use `--require-login` to see the sign-in screens.
- **First run on a server:** the first person to open the admin confirms their email with a code and becomes the site **owner**. A site published with `friendo deploy` already has you as owner.
- **Members and roles:** everyone with an account is a member. Roles add powers: owner > admin > editor > moderator > contributor. Contributors edit only their own posts; moderators approve and reject without editing; editors edit any. A site is one big group: groups use the same words. Admins add people from the admin (email only).
- **Presets:** Pick a site type (Personal / Community / Blog) in Settings to set who can sign up, who can post and what waits for review.
- **Signing in on pages:** visitors sign in with an email code through `<friendo-signin>` to comment, react, vote and RSVP.
- **Sync:** `friendo push --users` carries accounts (and any password hashes) to the deployed site.

Publishing to a network (friendo.world or your own) uses a separate **network account** — the same email-code sign-in, with browser device-auth for the CLI (`friendo login`). Its account page (`/account`, a `<friendo-account>` tag) lists your sites with an **Open admin** button that signs you into each one. Hosting sites for others just makes that account an **operator**; the operator's page is `/network` (`<friendo-console>`).

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
friendo push --posts        # also sync posts
friendo push --users        # also sync accounts and profiles
```

**`friendo pull`** — Pull remote data into your local database.

```bash
friendo pull --posts        # pull posts
friendo pull --users        # pull accounts and profiles
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
      <p>{{ post.body|truncatechars:200 }}</p>
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
| **Phase 3** | Community features — visitor comments/reactions/polls, realtime chats, locations, media, the `friendo.js` SDK | Complete |
| **v0.2 / v0.3** | Public-safe hardening, then consolidation onto one Go runtime + network mode: server-side community rendering, page-bundle galleries, render/CLI/SDK test coverage, `<friendo-map>`, profile switcher | Complete |
| **v0.4** | The network opens: quotas, account + site management, custom domains | Complete |
| **v0.5** | One sign-in (an emailed code everywhere, passwords opt-in); the network as a friendo site; members-only pages; a built-in calendar with RSVP; declared fields + feature switches + a rebuilt records admin | Complete |
| **v0.6** | Social graph — profiles at `/profiles/<slug>`, one-way follows, an in-page inbox, groups as a built-in collection, event invitations | Complete |
| **Phase 2** | Desktop editor (Tauri-based WYSIWYG) | Planned |

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
