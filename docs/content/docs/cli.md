---
title: CLI commands
slug: cli
group: Reference
weight: 8
---

The `friendo` command is a thin client — it scaffolds sites, runs them locally,
and syncs with any deployed site over its API. Run `friendo --help` or
`friendo <command> --help` for details, and `friendo --version` to check your
version.

## Building

### `friendo init [name]`
Scaffold a new site in `./name` (default `my-site`).

### `friendo serve`
Start the local dev server with hot reload. Compiles the `content/` folder on
startup and re-imports on every edit.

| Flag | Default | What |
|---|---|---|
| `--port`, `-p` | `3000` | Port to serve on |
| `--open-admin` | `false` | Skip admin auth (local convenience) |

### `friendo build`
Compile the [`content/`](/docs/content) folder (markdown files) into the site
database. Runs automatically inside `serve` and `push`/`deploy`; use it on its own
for one-off compiles or CI.

### `friendo export`
Export the site as static output.

| Flag | Default | What |
|---|---|---|
| `--mode` | `static` | `static` HTML, or a self-contained `bundle` |

## Deploying & syncing

### `friendo deploy [subdomain]`
Publish the current folder to a network (friendo.world by default), saving the
target to `friendo.toml`. Signs you in in the browser if needed, claims the
subdomain for your account, and pushes. `--network <url>` targets another network.

### `friendo push`
Push local state to your deployed site.

| Flag | What |
|---|---|
| `--data` | Also push records |
| `--users` | Also push user accounts |
| `--dry-run` | Show what would be pushed |
| `--target` | Override the deploy target URL |

### `friendo pull`
Pull remote state into your local database. Use `--data` for records and/or
`--users` for accounts (`--target` to override).

## Accounts & networks

Publishing goes to a **network** (friendo.world by default). You sign in to a
network with a passwordless browser flow — no platform password.

### `friendo login [network-url]`
Sign in to a network (default friendo.world) via browser device auth, so `deploy`
doesn't have to prompt.

### `friendo whoami [network-url]`
Show the account you're signed in as.

### `friendo logout`
Sign out — clear the cached network token and site sessions.

### `friendo network …`
Operator commands for running a network yourself — one host serving many sites by
subdomain. These need the **operator** capability on your account.

| Command | What |
|---|---|
| `friendo network serve` | Serve every site on the network by subdomain |
| `friendo network sites` | List the sites on the network |
| `friendo network provision <sub>` | Create a new site |
| `friendo network deploy <sub>` | Provision + push the current folder in one step |
| `friendo network destroy <sub>` | Delete a site and all its data (`--yes` to confirm) |
| `friendo network invite <email>` | Pre-create an account so it can sign in |
| `friendo network signups <open\|invite>` | Set who may create an account |
| `friendo network operator grant <email>` | Grant an account the operator capability |

## Config & auth

Deploy credentials are cached in `~/.friendo/config`. Site sync authenticates with
your **site admin** login (cached per site); signing in to a network is
passwordless — `friendo login` runs a browser device-auth flow, no platform
password. See [Auth & users](/docs/auth) for the distinction.
