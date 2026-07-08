---
title: Deploy
slug: deploy
group: Getting started
weight: 3
---

When your site is ready, one command puts it on the internet.

```bash
friendo deploy
```

It's an interactive wizard that asks where to host:

```
Where do you want to deploy?
  1. friendo.world (managed hosting)
  2. Cloudflare Workers (your own account)
  3. VPS / self-hosted
```

## friendo.world (managed)

Choosing **friendo.world** signs you in through your browser, then provisions a
site on a subdomain — `your-site.friendo.world` — with its own database and
asset storage, and pushes your templates and assets. You only run `deploy` once;
it saves the target in `friendo.toml`.

```bash
friendo deploy          # first time — provisions + pushes
friendo push            # push template/asset changes
friendo push --data     # also push your records
friendo push --users    # also push user accounts
```

friendo.world is convenient, not required — everything it does, you can do
yourself on your own infrastructure.

## Your own Cloudflare / a VPS

The same site runs unchanged on Cloudflare Workers (with D1 + R2) or on any
server via the `friendo` binary. `friendo deploy` prints the steps for each, and
`friendo push --target https://your-site.example.com` syncs to it afterward.

## Keeping in sync

All sync goes through your site's own API, authenticated with your site admin
login — so these work the same wherever the site is hosted:

| Command | What |
|---|---|
| `friendo push` | Upload templates + assets (`--data`, `--users` to include those) |
| `friendo pull --data` | Pull remote records back into your local database |
| `friendo pull --users` | Pull user accounts back down |
| `friendo redeploy` | Re-push the managed-hosting runtime to your site |
| `friendo destroy` | Tear the deployed site down completely |

## Export instead

Don't want a server at all? Export to static HTML:

```bash
friendo export --mode static
```

See the [CLI reference](/docs/cli) for every command and flag.
