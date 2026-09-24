---
title: A site is a folder
slug: site-is-a-folder
group: Concepts
weight: 4
---

The core idea in Friendo: **your site is a folder**. Templates, pages, assets,
and a database, all sitting in a directory you own. It's portable, checkable into
git, and outlives any single tool — including Friendo.

```
my-site/
├── friendo.toml     # config: name, content types, deploy target
├── layouts/         # shared layouts and partials
├── pages/           # page templates — file path maps to URL path
├── content/         # optional: content as markdown files (compiled into data/)
├── assets/          # static assets (CSS, JS, images)
└── data/            # the SQLite database
```

There's no build artifact and no hidden state. What you see in the folder is the
whole site.

## One runtime, everywhere

The thing that *runs* a site is the **runtime** — a single Go binary. There's
just one, and it runs the same everywhere: on your laptop, on a server you own,
and on friendo.world. Nothing to port between environments.

- **Runs as** one binary — `friendo serve` for a single site, or
  `friendo network serve` to host many sites by subdomain.
- **Data** lives in SQLite; **assets** in `assets/` on disk (or S3-compatible
  storage like Cloudflare R2 once you offload media).
- **Templates** are Pongo2 (Jinja2-compatible).

It exposes the same interface everywhere — your site at `/`, the admin UI at
`/_/`, and a REST/sync API at `/_/api/*` — so a site behaves the same whether
it's on your laptop, your own `friendo network`, or friendo.world.

## Portability first

- **Local before cloud.** Building never requires an account or an internet
  connection.
- **Honest templates.** What's in the editor is what's in the file — plain text,
  checkable into git.
- **No lock-in.** Export to static HTML anytime, or host the runtime yourself.
  friendo.world is a convenience, not a dependency.

Next: how [content](/docs/content) and [templates](/docs/templates) fit together.
