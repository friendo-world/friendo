---
title: CLI commands
slug: cli
section: Reference
weight: 8
---

The `friendo` command scaffolds sites, runs them locally, and syncs with any
deployed site over its API. Run `friendo --help` or `friendo <command> --help`
for details, and `friendo --version` to check your version.

## Building

### `friendo init [name]`
Scaffold a new site in `./name` (default `my-site`).

### `friendo serve`
Start the local dev server with hot reload. Reads the `content/` folder on startup
and again on every edit.

| Flag | Default | What |
|---|---|---|
| `--port`, `-p` | `3000` | Port to serve on |
| `--require-login` | `false` | Ask for a sign-in even on localhost. By default the admin opens without one for requests from your own machine when no email provider is configured |
| `--no-login` | `false` | Skip the admin sign-in for every request (never on a public site) |

### `friendo import`
Read the [`content/`](/docs/posts#writing-posts-in-files) folder (markdown files)
into the site's database. `serve`, `push` and `deploy` do it for you; use it on
its own for one-off imports or CI.

### `friendo export`
Write the site as [static files](/docs/static-export).

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
| `--posts` | Also push posts, with their events and group memberships |
| `--users` | Also push accounts, with their profiles and follows |
| `--dry-run` | Show what would be pushed |
| `--target` | Override the deploy target URL |

### `friendo pull`
Pull remote state into your local database. Use `--posts` for posts and `--users`
for accounts (`--target` to override). Every pull also rewrites the `[content]`
block of your `friendo.toml` to list the site's collections and the fields their
posts carry (see [friendo.toml](/docs/config#content)); the rest of the file is
left alone.

## Accounts & networks

Publishing goes to a **network** (friendo.world by default). You sign in to a
network with a passwordless browser flow.

### `friendo login [network-url]`
Sign in to a network (default friendo.world) via the browser, so `deploy` doesn't
have to prompt.

### `friendo whoami [network-url]`
Show the account you're signed in as.

### `friendo logout`
Sign out: clear the cached network token and site sessions.

### `friendo open-admin [subdomain]`
Open a site's admin in your browser, signed in from your network account, the
terminal twin of the **Open admin** button on your [account page](/docs/network-account).
Defaults to the site in the current folder; `--network <url>` for another network.

### `friendo network …`
Operator commands for running a network yourself: one host serving many sites by
subdomain. These need to be run by an **operator**. Every one of them works two
ways: on the box (`--root`, or `FRIENDO_NETWORK_ROOT`) or from anywhere with
`--network <url>` after `friendo login <url>`. The same levers are in the browser
at `/network`; see [Run a network](/docs/run-a-network).

| Command | What |
|---|---|
| `friendo network serve` | Serve every site on the network by subdomain |
| `friendo network home [sub]` | Show or set the site served at the bare domain (`--clear` for the built-in page) |
| `friendo network sites` | List the sites on the network, with owners and status |
| `friendo network provision <sub>` | Create a new site (`--owner <email>`, `--name`) |
| `friendo network deploy <sub>` | Provision + push the current folder in one step (`--owner`) |
| `friendo network sites suspend <sub>` | Show visitors a notice instead of the site (`--reason`) |
| `friendo network sites resume <sub>` | Put a suspended site back on the air |
| `friendo network destroy <sub>` | Delete a site and all its data (`--yes` to confirm) |
| `friendo network accounts` | List accounts with their sites and status |
| `friendo network accounts suspend <email>` | Block an account from signing in or creating sites (`--reason`) |
| `friendo network accounts resume <email>` | Let a suspended account back in |
| `friendo network accounts signout <email>` | Sign an account out of every device |
| `friendo network invite <email>` | Invite someone (`--days`, default 14) |
| `friendo network invites` | List outstanding invites (`revoke <email>`, `prune`) |
| `friendo network quota` | Show or set how many sites an account can create |
| `friendo network signups <open\|invite>` | Set who may create an account |
| `friendo network operator grant <email>` | Make an account an operator |
| `friendo network operator revoke <email>` | Stop an account being an operator |

### Limits and levers

Every account can create a limited number of sites, **3** by default, so opening
signups can't cost you without bound. Raise it for everyone with
`friendo network quota --default 10`, or for one person with
`friendo network quota ada@example.com 25`. Operators are never limited. Someone
who hits the cap is told what the limit is and that an operator can lift it.

**Suspending** is the reversible alternative to `destroy`. Suspending an *account*
blocks sign-in, new sites, and any session it already holds; suspending a *site*
serves visitors a notice. Neither deletes anything. Operators can't be suspended:
revoke them first.

### `friendo domain …`
Use your own domain for a site instead of its network address. These are **your**
commands, not the operator's; they run against the network with your own sign-in.

| Command | What |
|---|---|
| `friendo domain add <domain>` | Connect a domain and print the DNS records to add (`--site`) |
| `friendo domain verify <domain>` | Check the records are in place and go live |
| `friendo domain list` | Your domains and whether each is live |
| `friendo domain remove <domain>` | Disconnect it (the site keeps its network address) |

Adding a domain changes nothing on its own. Your site keeps serving at its network
address the whole time, and the new domain starts working only once you've added
the records and verified it; an unverified domain never serves traffic, which is
what stops someone claiming a domain that isn't theirs. Until then, anyone
visiting the domain sees a short page saying it isn't live yet and listing the
remaining steps.

## Where sign-ins are kept

Credentials are cached in `~/.friendo/config`. Site sync (`push`, `pull`) signs in
to the **site**, with a code sent to your email or a password where the site
allows one, and caches the session per site; `deploy` skips that by signing you in
from your network sign-in. Signing in to a **network** is `friendo login`, always
passwordless. See [Signing in](/docs/signing-in#your-site-vs-your-network-account)
for the distinction.
