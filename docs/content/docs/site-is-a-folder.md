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
├── templates/       # shared layouts and partials
├── pages/           # page templates — file path maps to URL path
├── public/          # static assets (CSS, JS, images)
└── data/            # the SQLite database
```

There's no build artifact and no hidden state. What you see in the folder is the
whole site.

## One site, two runtimes

The thing that *runs* a site is a **runtime**. Friendo has two, and they behave
identically:

| | Go runtime | Edge runtime |
|---|---|---|
| Runs as | a single binary (`friendo serve`), e.g. on a VPS | a Cloudflare Worker |
| Data | SQLite | D1 |
| Assets | `public/` on disk | R2 |
| Templates | Pongo2 (Jinja2) | a Jinja2-compatible engine |

Both expose the same interface — your site at `/`, the admin UI at `/_/`, and a
REST/sync API at `/_/api/*` — so a site behaves the same whether it's on your
laptop, your own Cloudflare account, or friendo.world.

## Portability first

- **Local before cloud.** Building never requires an account or an internet
  connection.
- **Honest templates.** What's in the editor is what's in the file — plain text,
  checkable into git.
- **No lock-in.** Export to static HTML anytime, or host the runtime yourself.
  friendo.world is a convenience, not a dependency.

Next: how [content](/docs/content) and [templates](/docs/templates) fit together.
