---
title: Keep a deployed site in sync
slug: push-and-pull
section: Guides
topic: Hosting
weight: 10
description: Push local changes up to a deployed site and pull posts and accounts back down.
---

To keep a deployed site in step with the folder on your laptop, you **push**
changes up and **pull** posts and accounts back down. Both go through the site's
own API, so they work the same on friendo.world, on your own network, or on a
single site you host yourself.

## Push your changes

From the site's folder:

```bash
friendo push            # templates and assets
friendo push --posts    # also posts, with their events and group memberships
friendo push --users    # also accounts, with their profiles and follows
```

Every push also:

- reads your `content/` folder into the local database first, so posts you wrote
  as files go up with `--posts`;
- sends the site's settings (who can sign up, what waits for review), so a push
  doesn't quietly reset them to the defaults.

`--posts` also carries media rows for images attached to posts, and each post's
[event](/docs/time) time. `--users` carries password hashes, so a password set
locally works on the deployed site too, if that site allows passwords.

To see what would go up without sending anything:

```bash
friendo push --dry-run
```

It prints the site, the number of posts and the number of files.

## Pull posts and accounts back

Posts written in the deployed site's admin, and people who signed up there, live
only on that site until you pull them:

```bash
friendo pull --posts    # posts, events, group memberships and media rows
friendo pull --users    # accounts, profiles and follows
```

`pull` needs at least one of the two flags. It writes into your local database,
updating posts and accounts that are already there, and also brings back the
site's settings.

Every pull also rewrites the `[content]` block of your `friendo.toml` to list the
site's collections and the fields their posts carry, so the folder knows about a
collection someone started on the site. The rest of the file is left alone.

> **Common mistake:** editing a post in the deployed site's admin, then running
> `friendo push --posts` without pulling. If your folder has that post too, your
> local copy goes up and replaces the edit. Pull first, then push. For a post
> written as a file in `content/`, make the edit in the file: every push reads
> the file again.

## Where it pushes to

`push` and `pull` send to the `target` under `[deploy]` in `friendo.toml`.
`friendo deploy` writes it for you the first time:

```toml
[deploy]
target = "https://my-club.friendo.world"
```

With no `target`, they use `https://<site name>.friendo.world`. To send somewhere
else once, without changing the file:

```bash
friendo push --target https://my-club.example.com
friendo pull --posts --target https://my-club.example.com
```

## Signing in to sync

`push` and `pull` sign in to the **site**, not the network. The first time, the
CLI asks for your email and the code the site sends you (or a password, if the
site allows passwords), then keeps the session in `~/.friendo/config`. A site
made with `friendo deploy` skips this: `deploy` signs you in from your network
account. See [Your site and the network](/docs/site-vs-network).

## Changes show up right away

Files in `assets/` are served so that browsers and CDNs check back with your site
on every request. A changed stylesheet or image is live as soon as you push it,
and an unchanged one costs only a tiny "not modified" reply.

To have a file cached for a long time instead, give it a version in the URL and
bump the number when it changes:

```html
<link rel="stylesheet" href="/assets/style.css?v=2">
```

A URL with `?v=` is cached for a year.

**Next:** [CLI commands](/docs/cli) for every flag,
[friendo.toml](/docs/config#deploy) for the `[deploy]` keys, and
[Custom domains](/docs/custom-domains) to put the site on your own address.
