---
title: Deploy
slug: deploy
group: Getting started
weight: 3
---

When your site is ready, one command puts it on the internet.

```bash
friendo deploy            # subdomain from your site's name
friendo deploy my-club    # or pick the subdomain
```

`deploy` publishes the current folder to a **network** — friendo.world by
default. If you're not signed in it opens your browser to sign you in (a one-time
device code, no password), then claims the subdomain for your account and pushes
your templates and assets. Point it at any other network with
`--network https://sites.example.com`.

## friendo.world (managed)

By default `deploy` targets **friendo.world**, which hosts your site on a
subdomain — `your-site.friendo.world` — with its own database and asset storage.
You only run `deploy` once; it saves the target in `friendo.toml`.

```bash
friendo deploy          # first time — provisions + pushes
friendo push            # push template/asset changes
friendo push --data     # also push your records
friendo push --users    # also push user accounts
```

Once it's up, **[friendo.world/account](https://friendo.world/account)** lists
your sites: open a site's admin from there (no separate sign-in), connect
[your own domain](/docs/custom-domains), or make another site. See
[Your network account](/docs/network-account).

friendo.world is convenient, not required — everything it does, you can do
yourself on your own infrastructure.

## Self-host it yourself

friendo.world runs the same `friendo` binary you already have. Serve one site
directly on a server you own ([Self-host a site](/docs/self-host-a-site)), or run
your own **network** — one host serving many sites by subdomain
([Run a network](/docs/run-a-network)) — then
`friendo deploy my-club --network https://sites.example.com` publishes to it.
`friendo push --target https://your-site.example.com` syncs to any site
afterward.

## Keeping in sync

All sync goes through your site's own API. `deploy` signs you into the network
and mints the site's admin session for you; for a self-hosted site the CLI asks
for your email and the code it sends you (or a password, if the site allows
them). Either way these work the same wherever the site is hosted:

| Command | What |
|---|---|
| `friendo push` | Upload templates + assets (`--data`, `--users` to include those) |
| `friendo pull --data` | Pull remote records back into your local database |
| `friendo pull --users` | Pull user accounts back down |

## Changes show up right away

Files in `assets/` are served so that browsers and CDNs check back with your
site on every request. A changed stylesheet or image is live as soon as you
push it, and an unchanged one costs only a tiny "not modified" reply. If you
want a file cached for a long time instead, give it a version in the URL,
`/assets/style.css?v=2`, and bump the number when it changes.

## Export instead

Don't want a server at all? Export to static HTML:

```bash
friendo export --mode static
```

See the [CLI reference](/docs/cli) for every command and flag.
