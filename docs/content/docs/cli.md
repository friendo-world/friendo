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

### `friendo deploy`
Interactive first-time setup — provisions a site on a hosting target, saves the
target to `friendo.toml`, and pushes. `--api-url` targets a non-default platform.

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

### `friendo redeploy`
Re-push the current managed-hosting runtime to your site's Worker. Your data is
untouched.

### `friendo destroy`
Deprovision the deployed site — deletes its Worker, database, and assets. Prompts
for confirmation; `--yes` skips it.

## Config & auth

Deploy credentials are cached in `~/.friendo/config`. Site sync authenticates with
your **site admin** login (cached per site); provisioning on friendo.world uses
your **platform** login. See [Auth & users](/docs/auth) for the distinction.
